import 'package:flutter_test/flutter_test.dart';
import 'package:neorouter/main_router_mascot.dart';
import 'package:neorouter/src/mascot/mascot_frames.dart';
import 'package:neorouter/src/mascot/mascot_mood.dart';

void main() {
  test('every mood has frames, including the stressed one', () {
    for (final mood in MascotMood.values) {
      expect(MascotClips.forMood(mood).frames, isNotEmpty);
      expect(MascotClips.keyFrame(mood).dots.length, MascotFrame.gridSize * MascotFrame.gridSize);
    }
  });

  test('routing load turns into idle, routing and stressed', () {
    final load = RouterLoad.instance;
    load
      ..inFlight = 0
      ..busiestAccount = 0;
    expect(load.mood, MascotMood.idle);
    load
      ..inFlight = 3
      ..busiestAccount = 2;
    expect(load.mood, MascotMood.working);
    load.busiestAccount = RouterLoad.stressedAccount;
    expect(load.mood, MascotMood.stressed);
    load
      ..inFlight = RouterLoad.stressedTotal
      ..busiestAccount = 1;
    expect(load.mood, MascotMood.stressed);
  });
}
