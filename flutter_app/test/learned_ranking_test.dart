import 'package:flutter_test/flutter_test.dart';
import 'package:neorouter/src/learned_ranking.dart';

Map<String, Object?> cell(String model, {double quality = 0.5, double speed = 0.5, double reliability = 0.95, int answers = 100, Map<String, Object?>? free}) => <String, Object?>{
  'task': 'coding',
  'model': model,
  'name': model,
  'answers': answers,
  'quality': quality,
  'speed': speed,
  'reliability': reliability,
  'free': free,
};

void main() {
  final fast = cell('fast', quality: 0.25, speed: 0.95);
  final smart = cell('smart', quality: 0.9, speed: 0.3);

  LearnedView rank(List<Map> cells, double position, {bool freeOnly = false}) =>
      rankLearned(cells: cells, tasks: const <String>['coding'], models: cells.map((c) => '${c['model']}').toList(), position: position, freeOnly: freeOnly);

  test('the slider decides what counts as best', () {
    expect(rank(<Map>[fast, smart], LearnedStyle.fast).best.single['model'], 'fast');
    expect(rank(<Map>[fast, smart], LearnedStyle.smart).best.single['model'], 'smart');
  });

  test('its stops are the routing styles', () {
    final balanced = learnedWeights(LearnedStyle.balanced);
    expect(balanced.quality, closeTo(0.35, 1e-9));
    expect(balanced.speed, closeTo(0.2, 1e-9));
    final quick = learnedWeights(LearnedStyle.fast);
    expect(quick.quality, closeTo(0.15, 1e-9));
    expect(quick.speed, closeTo(0.5, 1e-9));
    final clever = learnedWeights(LearnedStyle.smart);
    expect(clever.quality, closeTo(0.6, 1e-9));
    expect(clever.speed, closeTo(0.05, 1e-9));
  });

  test('free only leaves out paid models and uses the free copy of the rest', () {
    final both = cell('both', quality: 0.9, speed: 0.9, free: <String, Object?>{'quality': 0.4, 'speed': 0.4, 'reliability': 0.9});
    final view = rank(<Map>[smart, both], LearnedStyle.balanced, freeOnly: true);
    expect(view.best.single['model'], 'both');
    expect(view.best.single['quality'], 0.4);
    expect(view.models.map((m) => m['model']), <Object?>['both']);
    expect(rank(<Map>[smart], LearnedStyle.balanced, freeOnly: true).best, isEmpty);
  });

  test('one lucky answer cannot win while proven models exist', () {
    final lucky = cell('lucky', quality: 0.95, speed: 0.95, answers: 1);
    final proven = cell('proven', quality: 0.4, speed: 0.4, answers: 200);
    expect(rank(<Map>[lucky, proven], LearnedStyle.balanced).best.single['model'], 'proven');
    expect(rank(<Map>[lucky], LearnedStyle.balanced).best.single['model'], 'lucky');
  });
}
