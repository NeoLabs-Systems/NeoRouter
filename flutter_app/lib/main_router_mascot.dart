import 'dart:async';

import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'src/mascot/mascot_mood.dart';
import 'src/mascot/neo_mascot.dart';

/// How hard the router is working right now, read from `/routing/load`.
/// One poll for the whole app, running only while a mascot is on screen.
class RouterLoad extends ChangeNotifier {
  RouterLoad._();

  static final RouterLoad instance = RouterLoad._();

  static const Duration _every = Duration(seconds: 3);

  /// No requests for this long: the mascot dozes off.
  static const Duration _sleepAfter = Duration(minutes: 5);

  /// In flight at once from which the router looks stressed, overall or on one account.
  static const int stressedTotal = 8;
  static const int stressedAccount = 4;

  int inFlight = 0;
  int busiestAccount = 0;
  DateTime _lastBusy = DateTime.now();
  NeoRouterController? _controller;
  Timer? _timer;
  int _watchers = 0;

  MascotMood get mood {
    if (inFlight >= stressedTotal || busiestAccount >= stressedAccount) return MascotMood.stressed;
    if (inFlight > 0) return MascotMood.working;
    if (DateTime.now().difference(_lastBusy) > _sleepAfter) return MascotMood.asleep;
    return MascotMood.idle;
  }

  String describe(AppL10n t) => switch (mood) {
    MascotMood.stressed => t.mascotStressed('$inFlight'),
    MascotMood.working => t.mascotWorking('$inFlight'),
    MascotMood.asleep => t.mascotAsleep,
    _ => t.mascotIdle,
  };

  void watch(NeoRouterController controller) {
    _controller = controller;
    if (_watchers++ == 0) {
      _poll();
      _timer = Timer.periodic(_every, (_) => _poll());
    }
  }

  void unwatch() {
    if (--_watchers == 0) {
      _timer?.cancel();
      _timer = null;
    }
  }

  Future<void> _poll() async {
    final controller = _controller;
    if (controller == null) return;
    try {
      final load = await controller.api.routingLoad();
      final accounts = ((load['accounts'] as Map?) ?? const <String, Object?>{}).values.whereType<num>().map((value) => value.toInt());
      final total = accounts.fold<int>(0, (sum, value) => sum + value);
      final busiest = accounts.fold<int>(0, (top, value) => value > top ? value : top);
      if (total > 0) _lastBusy = DateTime.now();
      final before = mood;
      final changed = total != inFlight || busiest != busiestAccount;
      inFlight = total;
      busiestAccount = busiest;
      if (changed || mood != before) notifyListeners();
    } catch (_) {
      // No permission or briefly unreachable: keep the last reading.
    }
  }
}

/// The mascot showing [RouterLoad]: calm when idle, busy while routing,
/// sweating when requests pile up, asleep after a quiet while.
class RouterMascot extends StatefulWidget {
  const RouterMascot({super.key, required this.controller, required this.size, this.tooltip = true});

  final NeoRouterController controller;
  final double size;
  final bool tooltip;

  @override
  State<RouterMascot> createState() => _RouterMascotState();
}

class _RouterMascotState extends State<RouterMascot> {
  final MascotMoodStabilizer _stabilizer = MascotMoodStabilizer();
  final RouterLoad _load = RouterLoad.instance;
  Timer? _settle;

  @override
  void initState() {
    super.initState();
    _load
      ..watch(widget.controller)
      ..addListener(_changed);
    _read();
  }

  @override
  void dispose() {
    _settle?.cancel();
    _load
      ..removeListener(_changed)
      ..unwatch();
    super.dispose();
  }

  void _changed() {
    final before = _stabilizer.mood;
    _read();
    if (_stabilizer.mood != before && mounted) setState(() {});
  }

  void _read() {
    _settle?.cancel();
    final wait = _stabilizer.update(_load.mood);
    if (wait != null) _settle = Timer(wait, _changed);
  }

  @override
  Widget build(BuildContext context) {
    final face = NeoMascot(mood: _stabilizer.mood, size: widget.size);
    if (!widget.tooltip) return face;
    return ListenableBuilder(
      listenable: _load,
      builder: (context, child) => Tooltip(message: _load.describe(AppL10n.of(context)), child: child),
      child: face,
    );
  }
}
