import 'dart:math' as math;

/// Where the routing styles sit on the slider from speed (0) to quality (1).
class LearnedStyle {
  const LearnedStyle._();

  static const double fast = 0;
  static const double balanced = 0.5;
  static const double smart = 1;
}

const int _maxTasks = 8;
const int _maxModels = 8;
// A model with fewer answers only wins a kind of work when nothing has more.
const int _minAnswersToWin = 5;
// Same floor as routing (scoring.js), so one zero cannot wipe out a score.
const double _floor = 0.02;

/// Weights of quality, speed and reliability at a slider position. The stops
/// are the routing styles (server/services/routing/profiles.js, without price);
/// between them they blend.
({double quality, double speed, double reliability}) learnedWeights(double position) {
  final p = position.clamp(0.0, 1.0);
  const reliability = 0.3;
  if (p <= LearnedStyle.balanced) {
    final share = p / LearnedStyle.balanced;
    return (quality: 0.15 + (0.35 - 0.15) * share, speed: 0.5 + (0.2 - 0.5) * share, reliability: reliability);
  }
  final share = (p - LearnedStyle.balanced) / (LearnedStyle.smart - LearnedStyle.balanced);
  return (quality: 0.35 + (0.6 - 0.35) * share, speed: 0.2 + (0.05 - 0.2) * share, reliability: reliability);
}

double _num(Object? value) => value is num ? value.toDouble() : 0;

/// The weighted geometric mean routing uses, so a model has to be decent on
/// every part (scoring.js, worth).
double learnedScore({required double quality, required double speed, required double reliability, required double position}) {
  final w = learnedWeights(position);
  final total = w.quality + w.speed + w.reliability;
  return math.exp(
    (w.quality / total) * math.log(math.max(quality, _floor)) +
        (w.speed / total) * math.log(math.max(speed, _floor)) +
        (w.reliability / total) * math.log(math.max(reliability, _floor)),
  );
}

class LearnedView {
  const LearnedView({required this.tasks, required this.models, required this.cells, required this.best});

  final List<String> tasks;
  final List<Map<String, Object?>> models;
  final List<Map<String, Object?>> cells;
  final List<Map<String, Object?>> best;
}

/// Ranks what was learned for one slider position. `tasks` and `models` come
/// busiest first.
LearnedView rankLearned({required List<Map> cells, required List<String> tasks, required List<String> models, required double position, required bool freeOnly}) {
  final scored = <Map<String, Object?>>[];
  for (final cell in cells) {
    final free = cell['free'];
    if (freeOnly && free is! Map) continue;
    final parts = freeOnly ? free as Map : cell;
    final quality = _num(parts['quality']);
    final speed = _num(parts['speed']);
    scored.add(<String, Object?>{
      ...cell.cast<String, Object?>(),
      'quality': quality,
      'speed': speed,
      'score': learnedScore(quality: quality, speed: speed, reliability: _num(parts['reliability']), position: position),
    });
  }
  final shownTasks = tasks.where((task) => scored.any((cell) => cell['task'] == task)).take(_maxTasks).toList();
  final best = <Map<String, Object?>>[];
  for (final task in shownTasks) {
    final candidates = scored.where((cell) => cell['task'] == task).toList();
    final proven = candidates.where((cell) => _num(cell['answers']) >= _minAnswersToWin).toList();
    final pool = proven.isEmpty ? candidates : proven;
    pool.sort((a, b) {
      final byScore = _num(b['score']).compareTo(_num(a['score']));
      return byScore != 0 ? byScore : _num(b['answers']).compareTo(_num(a['answers']));
    });
    final top = pool.first;
    best.add(<String, Object?>{...top, 'modelAnswers': top['answers']});
  }
  // Each kind of work's best model always gets a column, then the busiest.
  final known = scored.map((cell) => cell['model']).toSet();
  final columnIds = <String>{for (final item in best) '${item['model']}', ...models.where(known.contains)}.take(_maxModels).toList();
  final nameOf = <String, Object?>{for (final cell in scored) '${cell['model']}': cell['name']};
  return LearnedView(
    tasks: shownTasks,
    models: <Map<String, Object?>>[for (final id in columnIds) <String, Object?>{'model': id, 'name': nameOf[id]}],
    cells: scored.where((cell) => shownTasks.contains(cell['task']) && columnIds.contains(cell['model'])).toList(),
    best: best,
  );
}
