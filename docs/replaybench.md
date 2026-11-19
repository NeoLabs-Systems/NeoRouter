# NeoRouter ReplayBench

ReplayBench compares answers from two models on the same task sampled from
NeoRouter traffic. It reports a preference rate for each model and task category.

## Evaluation

The router replays the task through two distinct models using the originating
key's routing profile at low priority. Model selection depends on the
connections and free capacity available to the router.

ReplayBench checks basic response constraints, such as parseable JSON and
required tool calls. If just one answer satisfies those constraints, it wins.
If both fail, the comparison is discarded.

A third model, selected through `auto:smart`, judges the pair when both answers
pass the applicable checks. The judge must first pass two known-answer examples
in both answer orders. For each scored pair, ReplayBench hides the model IDs and
randomizes answer order. The judge compares correctness, use of evidence and
completion of the task. Abstentions and low-confidence verdicts are excluded.

## Scoring

A win counts as one point, a tie as half a point and a loss as zero.
Constraint-based verdicts carry full weight and judge verdicts carry a weight
of 0.85. The preference rate is a weighted average with a 30-day half-life.

Results use the task category recognized during routing, such as coding,
translation, math or summarization. Select a category to rank models by
preference rate. A model needs eight scored comparisons in that category to
appear. The rate reflects the specific tasks and opponents it faced. The ranked
list shows each model's comparison count alongside its score.

Expand a model to see the evaluation period and each evaluator's comparison
count, preference rate and rubric version.
