package task

import (
	"errors"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	sqlite "modernc.org/sqlite"
)

// What NeoRouter learned about a model on one kind of task: answers that
// worked or failed (empty, broken tool calls, cut off) and feedback from apps
// (POST /v1/feedback). Counts fade with a week's half-life, so a model that
// improves earns its way back.
const (
	halfLifeMs = 7 * 24 * 60 * 60 * 1000
	// Evidence needed before task experience counts as much as the general
	// track record.
	outcomeConfidence  = 8
	feedbackConfidence = 5
)

// sqliteConstraintForeignKey is SQLITE_CONSTRAINT_FOREIGNKEY: the model was
// removed while a request to it ran, so there is nothing left to learn about.
const sqliteConstraintForeignKey = 787

// isForeignKeyError reports the constraint violation the Node code swallows.
func isForeignKeyError(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintForeignKey
}

// TaskRow is one model's record for one kind of task.
type TaskRow struct {
	ModelRef  int64
	Task      string
	Successes float64
	Failures  float64
	Liked     float64
	Disliked  float64
	UpdatedAt int64
}

// Standing is what the counts add up to.
type Standing struct {
	Rate    float64
	Share   float64
	Quality float64
}

// Experience is the per-task track record store. It caches rows and loads a
// whole task kind with one query the first time it comes up (routing reads
// every model of a request).
type Experience struct {
	db *dbx.DB

	mu          sync.Mutex
	cache       map[string]*TaskRow
	loadedTasks map[string]bool
}

// NewExperience builds the store.
func NewExperience(db *dbx.DB) *Experience {
	return &Experience{db: db, cache: map[string]*TaskRow{}, loadedTasks: map[string]bool{}}
}

func experienceKey(ref int64, task string) string {
	return strconv.FormatInt(ref, 10) + "|" + task
}

func (e *Experience) load(ref int64, task string) *TaskRow {
	key := experienceKey(ref, task)
	if row, ok := e.cache[key]; ok {
		return row
	}
	if !e.loadedTasks[task] {
		e.loadedTasks[task] = true
		if e.db != nil {
			rows, err := e.db.R.Query(`SELECT model_ref, task, successes, failures, liked, disliked, updated_at FROM model_task_stats WHERE task = ?`, task)
			if err == nil {
				for rows.Next() {
					row := &TaskRow{}
					if err := rows.Scan(&row.ModelRef, &row.Task, &row.Successes, &row.Failures, &row.Liked, &row.Disliked, &row.UpdatedAt); err == nil {
						e.cache[experienceKey(row.ModelRef, row.Task)] = row
					}
				}
				rows.Close()
			}
		}
		if row, ok := e.cache[key]; ok {
			return row
		}
	}
	row := &TaskRow{ModelRef: ref, Task: task, UpdatedAt: time.Now().UnixMilli()}
	e.cache[key] = row
	return row
}

func decayTaskRow(row *TaskRow, now time.Time) *TaskRow {
	factor := pow05(float64(now.UnixMilli()-row.UpdatedAt) / halfLifeMs)
	row.Successes *= factor
	row.Failures *= factor
	row.Liked *= factor
	row.Disliked *= factor
	row.UpdatedAt = now.UnixMilli()
	return row
}

func (e *Experience) save(row *TaskRow) error {
	if e.db == nil {
		return nil
	}
	_, err := e.db.W.Exec(`INSERT INTO model_task_stats (model_ref, task, successes, failures, liked, disliked, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(model_ref, task) DO UPDATE SET successes = excluded.successes, failures = excluded.failures,
		liked = excluded.liked, disliked = excluded.disliked, updated_at = excluded.updated_at`,
		row.ModelRef, row.Task, row.Successes, row.Failures, row.Liked, row.Disliked, row.UpdatedAt)
	return err
}

func (e *Experience) update(ref int64, task string, mutate func(*TaskRow), now time.Time) {
	if ref == 0 || task == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	row := decayTaskRow(e.load(ref, task), now)
	mutate(row)
	if err := e.save(row); err != nil {
		// The model is gone (its connection was removed, a pool partner left):
		// nothing left to learn about, and never a reason to fail a request.
		if !isForeignKeyError(err) {
			return
		}
		delete(e.cache, experienceKey(ref, task))
	}
}

// RecordOutcome notes an answer that worked or failed.
func (e *Experience) RecordOutcome(ref int64, task string, ok bool, now time.Time) {
	e.update(ref, task, func(row *TaskRow) {
		if ok {
			row.Successes++
		} else {
			row.Failures++
		}
	}, now)
}

// RecordFeedback notes an app's rating of an answer.
func (e *Experience) RecordFeedback(ref int64, task string, good bool, now time.Time) {
	e.update(ref, task, func(row *TaskRow) {
		if good {
			row.Liked++
		} else {
			row.Disliked++
		}
	}, now)
}

// Faded is a stored row as it stands now, without changing it.
func Faded(row TaskRow, now time.Time) TaskRow {
	return *decayTaskRow(&row, now)
}

// StandingOf is what the counts add up to: the success rate, how much evidence
// stands behind it, and the quality factor from what people said.
func StandingOf(row TaskRow) Standing {
	outcomes := row.Successes + row.Failures
	rate := (row.Successes + 2) / (outcomes + 3)
	share := outcomes / (outcomes + outcomeConfidence)
	votes := row.Liked + row.Disliked
	quality := 1 + (votes/(votes+feedbackConfidence))*((row.Liked+1)/(votes+2)-0.5)*0.3
	return Standing{Rate: rate, Share: share, Quality: quality}
}

// Adjust is how the model does on this task, for scoring: reliability blended
// towards its success rate on the task, and a quality factor from feedback
// (1 = none).
func (e *Experience) Adjust(ref int64, task string, reliability float64, now time.Time) (adjustedReliability, quality float64) {
	if ref == 0 || task == "" {
		return reliability, 1
	}
	e.mu.Lock()
	row := *decayTaskRow(e.load(ref, task), now)
	e.mu.Unlock()
	standing := StandingOf(row)
	return (1-standing.Share)*reliability + standing.Share*standing.Rate, standing.Quality
}

// ResetForTests forgets what was cached.
func (e *Experience) ResetForTests() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = map[string]*TaskRow{}
	e.loadedTasks = map[string]bool{}
}

// pow05 is 0.5 ** exponent, the Node code's decay factor.
func pow05(exponent float64) float64 { return math.Pow(0.5, exponent) }
