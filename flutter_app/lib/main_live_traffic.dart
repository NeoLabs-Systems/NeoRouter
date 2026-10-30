import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' show PointMode;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// The home screen's hero: the user's own traffic flowing through the
/// router. Requests from the request log travel from the key that sent them
/// into the router, circle it while it decides, fly to the provider that
/// answered (bouncing back from the ones that failed first) and return as
/// answers. New requests play as they arrive; in between, recent ones are
/// replayed at an easy pace, and the view says which it is showing.
class LiveTrafficHero extends StatefulWidget {
  const LiveTrafficHero({super.key, required this.controller, required this.accounts, required this.keys, required this.title, required this.status, this.action, this.onMore});

  final NeoRouterController controller;
  final List<ProviderAccount> accounts;
  final List<RouterKey> keys;
  final String title;
  final String status;
  final Widget? action;
  final VoidCallback? onMore;

  @override
  State<LiveTrafficHero> createState() => _LiveTrafficHeroState();
}

// How long each leg of a request's trip takes on screen, in ms.
const double _inMs = 950, _thinkMs = 750, _outMs = 620, _bounceMs = 520, _rethinkMs = 420, _backMs = 600, _homeMs = 820;
const int _historySize = 40;

class _Attempt {
  const _Attempt(this.provider, this.ok);
  final int provider;
  final bool ok;
}

class _Packet {
  _Packet({required this.app, required this.attempts, required this.label, required this.success, required this.latency, required this.live});

  final int app;
  final List<_Attempt> attempts;
  final String label;
  final bool success;
  final int latency;
  final bool live;

  String phase = 'in';
  double t0 = 0;
  double dur = _inMs;
  int attempt = 0;

  int get provider => attempts.isEmpty ? -1 : attempts[attempt].provider;
}

class _Chip {
  _Chip(this.text, this.t0, this.dx);
  final String text;
  final double t0;
  final double dx;
}

/// The running scene: packets, glows and the bookkeeping the painter and
/// node widgets read every frame.
class _Scene extends ChangeNotifier {
  double t = 0;
  final List<_Packet> packets = <_Packet>[];
  final List<double> pulses = <double>[];
  final List<_Chip> chips = <_Chip>[];
  List<double> appGlow = <double>[];
  List<double> provGlow = <double>[];
  List<double> appFlash = <double>[];
  List<double> provFail = <double>[];
  List<int> provBusy = <int>[];
  final math.Random random = math.Random();

  void resize(int apps, int providers) {
    if (appGlow.length != apps) {
      appGlow = List<double>.filled(apps, 0);
      appFlash = List<double>.filled(apps, -1e9);
    }
    if (provGlow.length != providers) {
      provGlow = List<double>.filled(providers, 0);
      provFail = List<double>.filled(providers, -1e9);
      provBusy = List<int>.filled(providers, 0);
    }
  }

  int get thinking => packets.where((p) => p.phase == 'think' || p.phase == 'rethink').length;

  void add(_Packet p) {
    p.t0 = t;
    packets.add(p);
    if (p.app < appGlow.length) {
      appGlow[p.app] = 1;
      appFlash[p.app] = t;
    }
  }

  void advance(double dt) {
    t += dt;
    for (final p in packets) {
      var guard = 0;
      while (p.phase != 'done' && t - p.t0 >= p.dur && guard < 4) {
        _next(p);
        guard += 1;
      }
    }
    packets.removeWhere((p) => p.phase == 'done');
    final decay = dt / 900;
    for (var i = 0; i < appGlow.length; i++) {
      appGlow[i] = math.max(0, appGlow[i] - decay);
    }
    for (var j = 0; j < provGlow.length; j++) {
      provGlow[j] = math.max(0, provGlow[j] - decay);
    }
    pulses.removeWhere((start) => t - start > 1500);
    chips.removeWhere((chip) => t - chip.t0 > 1700);
    notifyListeners();
  }

  void _go(_Packet p, String phase, double dur) {
    p.phase = phase;
    p.t0 = t;
    p.dur = dur;
  }

  void _next(_Packet p) {
    switch (p.phase) {
      case 'in':
        _go(p, 'think', _thinkMs);
        pulses.add(t);
        // One label at a time, so busy moments stay readable.
        if (chips.isEmpty || t - chips.last.t0 > 450) chips.add(_Chip(p.label, t, (random.nextDouble() - 0.5) * 90));
      case 'think':
      case 'rethink':
        if (p.provider < 0) {
          // Nothing could take it: straight back as a failure.
          _go(p, 'fail', _homeMs);
          return;
        }
        _go(p, 'out', _outMs);
        if (p.provider < provGlow.length) provGlow[p.provider] = 1;
      case 'out':
        final attempt = p.attempts[p.attempt];
        if (!attempt.ok) {
          if (attempt.provider < provFail.length) provFail[attempt.provider] = t;
          _go(p, 'bounce', _bounceMs);
          return;
        }
        _go(p, 'work', (p.latency / 4).clamp(240, 1500).toDouble());
        if (p.provider < provBusy.length) provBusy[p.provider] += 1;
      case 'bounce':
        if (p.attempt + 1 < p.attempts.length) {
          p.attempt += 1;
          _go(p, 'rethink', _rethinkMs);
          pulses.add(t);
        } else {
          _go(p, 'fail', _homeMs);
        }
      case 'work':
        if (p.provider < provBusy.length) provBusy[p.provider] = math.max(0, provBusy[p.provider] - 1);
        _go(p, 'back', _backMs);
        if (p.provider < provGlow.length) provGlow[p.provider] = 1;
      case 'back':
        _go(p, 'home', _homeMs);
        if (p.app < appGlow.length) appGlow[p.app] = 1;
      case 'home':
      case 'fail':
        p.phase = 'done';
        if (p.app < appFlash.length) appFlash[p.app] = t;
    }
  }
}

/// Where everything sits for a given size: side by side when there is room,
/// apps on top and providers below on a phone.
class _Layout {
  _Layout(this.size, int apps, int providers) {
    vertical = size.width < 640;
    const pad = 16.0;
    if (!vertical) {
      nodeW = (size.width * 0.16).clamp(124, 200).toDouble();
      nodeH = 50;
      radius = (math.min(size.width, size.height) * 0.1).clamp(40, 74).toDouble();
      core = Offset(size.width / 2, size.height / 2);
      appPos = _column(apps, pad + nodeW / 2, 84);
      provPos = _column(providers, size.width - pad - nodeW / 2, 72);
    } else {
      nodeH = 44;
      radius = (size.width * 0.13).clamp(34, 56).toDouble();
      core = Offset(size.width / 2, size.height * 0.46);
      appPos = _row(apps, pad + nodeH / 2, pad);
      final perRow = (providers / 2).ceil();
      provPos = <Offset>[..._row(math.min(providers, perRow), size.height - pad - nodeH * 1.5 - 8, pad), ..._row(providers - math.min(providers, perRow), size.height - pad - nodeH / 2, pad)];
      // Each row's cards share its width.
      appW = (size.width - pad * 2 - (apps - 1) * 8) / math.max(1, apps);
      provW = (size.width - pad * 2 - (perRow - 1) * 8) / math.max(1, perRow);
      nodeW = math.min(appW, provW);
      return;
    }
    appW = nodeW;
    provW = nodeW;
  }

  final Size size;
  late final bool vertical;
  late double nodeW;
  late final double appW;
  late final double provW;
  late final double nodeH;
  late final double radius;
  late final Offset core;
  late final List<Offset> appPos;
  late final List<Offset> provPos;

  double get orbit => radius * 1.32;

  List<Offset> _column(int count, double x, double maxGap) {
    if (count == 0) return <Offset>[];
    final gap = math.min(maxGap, (size.height - 40) / count);
    final top = size.height / 2 - gap * (count - 1) / 2;
    return <Offset>[for (var i = 0; i < count; i++) Offset(x, top + gap * i)];
  }

  List<Offset> _row(int count, double y, double pad) {
    if (count == 0) return <Offset>[];
    final width = (size.width - pad * 2) / count;
    return <Offset>[for (var i = 0; i < count; i++) Offset(pad + width * (i + 0.5), y)];
  }

  // Cubic curves from an app into the router and from the router to a provider.
  List<Offset> appCurve(int i) {
    final a = appPos[i];
    if (vertical) {
      final p0 = Offset(a.dx, a.dy + nodeH / 2), p3 = Offset(core.dx, core.dy - radius);
      final dy = p3.dy - p0.dy;
      return <Offset>[p0, Offset(p0.dx, p0.dy + dy * 0.5), Offset(p3.dx, p3.dy - dy * 0.4), p3];
    }
    final p0 = Offset(a.dx + nodeW / 2, a.dy), p3 = Offset(core.dx - radius, core.dy);
    final dx = p3.dx - p0.dx;
    return <Offset>[p0, Offset(p0.dx + dx * 0.5, p0.dy), Offset(p3.dx - dx * 0.4, p3.dy), p3];
  }

  List<Offset> provCurve(int j) {
    final p = provPos[j];
    if (vertical) {
      final p0 = Offset(core.dx, core.dy + radius), p3 = Offset(p.dx, p.dy - nodeH / 2);
      final dy = p3.dy - p0.dy;
      return <Offset>[p0, Offset(p0.dx, p0.dy + dy * 0.4), Offset(p3.dx, p3.dy - dy * 0.5), p3];
    }
    final p0 = Offset(core.dx + radius, core.dy), p3 = Offset(p.dx - nodeW / 2, p.dy);
    final dx = p3.dx - p0.dx;
    return <Offset>[p0, Offset(p0.dx + dx * 0.4, p0.dy), Offset(p3.dx - dx * 0.5, p3.dy), p3];
  }

  // Requests enter on one side of the router and leave on the other.
  double get inAngle => vertical ? -math.pi / 2 : math.pi;
  double get outAngle => vertical ? math.pi / 2 : 0;

  Offset onOrbit(double angle) => core + Offset(math.cos(angle) * orbit, math.sin(angle) * orbit * 0.92);
}

Offset _bez(List<Offset> c, double t) {
  final u = 1 - t;
  return c[0] * (u * u * u) + c[1] * (3 * u * u * t) + c[2] * (3 * u * t * t) + c[3] * (t * t * t);
}

double _ease(double t) {
  final x = t.clamp(0.0, 1.0);
  return x < 0.5 ? 4 * x * x * x : 1 - math.pow(-2 * x + 2, 3).toDouble() / 2;
}

class _LiveTrafficHeroState extends State<LiveTrafficHero> with SingleTickerProviderStateMixin, AutomaticKeepAliveClientMixin {
  // Home is a scrolling list: without this, scrolling down to the details
  // throws the view away and it starts over, empty, on the way back.
  @override
  bool get wantKeepAlive => true;

  bool _loaded = false;

  final _Scene _scene = _Scene();
  late final Ticker _ticker;
  Duration _last = Duration.zero;
  Timer? _poll;
  final Set<String> _seen = <String>{};
  final List<RequestLog> _history = <RequestLog>[];
  final List<RequestLog> _queue = <RequestLog>[];
  double _sinceSpawn = 0;
  double _nextReplay = 600;
  double _lastLive = -1e9;
  bool _warmed = false;

  List<String> _apps = <String>[];
  List<String> _providers = <String>[];
  List<String> _providerSlugs = <String>[];
  final Map<String, int> _appRequests = <String, int>{};
  final Map<int, String> _lastModel = <int, String>{};

  @override
  void initState() {
    super.initState();
    _ticker = createTicker(_tick)..start();
    _fetch();
    _poll = Timer.periodic(const Duration(seconds: 4), (_) => _fetch());
  }

  // The home screen loads connections and keys after this view starts.
  @override
  void didUpdateWidget(LiveTrafficHero oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.accounts != widget.accounts || oldWidget.keys != widget.keys) {
      _arrange();
      if (_history.isNotEmpty && !_warmed) _warmUp();
    }
  }

  @override
  void dispose() {
    _poll?.cancel();
    _ticker.dispose();
    _scene.dispose();
    super.dispose();
  }

  Future<void> _fetch() async {
    try {
      final logs = await widget.controller.api.logs(limit: _historySize);
      if (!mounted) return;
      _loaded = true;
      final fresh = logs.where((log) => !_seen.contains(log.id)).toList();
      _seen.addAll(fresh.map((log) => log.id));
      final first = _history.isEmpty;
      _history
        ..insertAll(0, fresh)
        ..length = math.min(_history.length, _historySize);
      // The first load is history to replay; later arrivals play live.
      if (!first) _queue.addAll(fresh.reversed.take(30));
      _arrange();
      if (first && !_warmed) _warmUp();
    } catch (_) {
      // The next poll tries again; the scene keeps replaying meanwhile.
    }
  }

  // Which apps (keys) and connections get a place: the busiest first.
  void _arrange() {
    final keyCounts = <String, int>{};
    final slugCounts = <String, int>{};
    for (final log in _history) {
      final key = log.keyName ?? '';
      if (key.isNotEmpty) keyCounts[key] = (keyCounts[key] ?? 0) + 1;
      for (final slug in _slugsOf(log)) {
        slugCounts[slug] = (slugCounts[slug] ?? 0) + 1;
      }
    }
    final keyNames = <String>{...(keyCounts.keys.toList()..sort((a, b) => keyCounts[b]!.compareTo(keyCounts[a]!))), ...widget.keys.map((key) => key.name)}.toList();
    final wide = MediaQuery.maybeSizeOf(context)?.width ?? 1200;
    final maxApps = wide < 640 ? 4 : 6;
    _apps = keyNames.length > maxApps ? <String>[...keyNames.take(maxApps - 1), _otherLabel] : keyNames;
    final enabled = widget.accounts.where((account) => account.enabled).toList()..sort((a, b) => (slugCounts[b.slug] ?? 0).compareTo(slugCounts[a.slug] ?? 0));
    final maxProviders = 8;
    final shown = enabled.length > maxProviders ? enabled.take(maxProviders - 1).toList() : enabled;
    _providerSlugs = shown.map((account) => account.slug).toList();
    // Custom endpoints share one generic label; their own name tells them apart.
    _providers = shown.map((account) => account.definitionId.startsWith('custom') || account.label.isEmpty ? account.slug : account.label).toList();
    if (enabled.length > maxProviders) {
      _providerSlugs.add('*');
      _providers.add(_otherLabel);
    }
    _scene.resize(math.max(1, _apps.length), math.max(1, _providers.length));
  }

  String get _otherLabel => AppL10n.of(context).liveOther;

  Iterable<String> _slugsOf(RequestLog log) sync* {
    for (final step in log.trail) {
      final model = '${step['model'] ?? ''}';
      if (model.contains('/')) yield model.split('/').first;
    }
    if (log.trail.isEmpty && (log.model ?? '').contains('/')) yield log.model!.split('/').first;
  }

  int _providerIndex(String slug) {
    final index = _providerSlugs.indexOf(slug);
    if (index >= 0) return index;
    return _providerSlugs.indexOf('*');
  }

  _Packet? _packetFor(RequestLog log, {required bool live}) {
    var app = _apps.indexOf(log.keyName ?? '');
    if (app < 0) app = _apps.indexOf(_otherLabel);
    if (app < 0) app = 0;
    final attempts = <_Attempt>[];
    for (final step in log.trail) {
      final outcome = '${step['outcome'] ?? ''}';
      if (outcome == 'hedge_lost') continue;
      final model = '${step['model'] ?? ''}';
      final provider = model.contains('/') ? _providerIndex(model.split('/').first) : -1;
      if (provider < 0) continue;
      attempts.add(_Attempt(provider, outcome == 'success'));
      if (outcome == 'success') _lastModel[provider] = model.substring(model.indexOf('/') + 1);
    }
    if (attempts.isEmpty && log.success && (log.model ?? '').contains('/')) {
      final provider = _providerIndex(log.model!.split('/').first);
      if (provider >= 0) attempts.add(_Attempt(provider, true));
    }
    // A failed request ends on its last failed attempt; a successful one on a success.
    if (log.success && attempts.isNotEmpty && !attempts.last.ok) attempts.add(_Attempt(attempts.last.provider, true));
    final task = log.task ?? log.capability;
    return _Packet(app: app, attempts: attempts, label: task.isEmpty ? 'chat' : task.replaceAll('_', ' '), success: log.success, latency: log.latencyMs ?? 900, live: live);
  }

  void _spawn(RequestLog log, {required bool live}) {
    final packet = _packetFor(log, live: live);
    if (packet == null) return;
    final key = _apps.isEmpty ? '' : _apps[packet.app];
    _appRequests[key] = (_appRequests[key] ?? 0) + 1;
    _scene.add(packet);
    if (live) _lastLive = _scene.t;
  }

  // Starts mid-traffic, so the very first frame already has requests moving.
  void _warmUp() {
    _warmed = true;
    for (var i = 0; i < 300; i++) {
      _step(16);
    }
  }

  double get _speed => MediaQuery.maybeDisableAnimationsOf(context) == true ? 0.5 : 1;

  void _tick(Duration elapsed) {
    final dt = math.min(100.0, (elapsed - _last).inMicroseconds / 1000);
    _last = elapsed;
    _step(dt * _speed);
  }

  void _step(double dt) {
    _sinceSpawn += dt;
    if (_queue.isNotEmpty && _sinceSpawn > 260) {
      _sinceSpawn = 0;
      _spawn(_queue.removeAt(0), live: true);
    } else if (_queue.isEmpty && _history.isNotEmpty && _sinceSpawn > _nextReplay && _scene.packets.length < 24) {
      _sinceSpawn = 0;
      _nextReplay = 700 + _scene.random.nextDouble() * 1300;
      _spawn(_history[_scene.random.nextInt(_history.length)], live: false);
    }
    _scene.advance(dt);
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    final palette = neoRouterPaletteOf(context);
    return Container(
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: palette.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: LayoutBuilder(
        builder: (context, box) {
          final compact = box.maxWidth < 640;
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              _Header(title: widget.title, status: widget.status, action: widget.action, scene: _scene, liveSince: () => _lastLive, hasTraffic: () => _history.isNotEmpty, compact: compact),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, stage) {
                    final layout = _Layout(Size(stage.maxWidth, stage.maxHeight), math.max(1, _apps.length), math.max(1, _providers.length));
                    return _Stage(scene: _scene, layout: layout, apps: _apps, providers: _providers, appRequests: _appRequests, lastModel: _lastModel, empty: _history.isEmpty, loaded: _loaded);
                  },
                ),
              ),
              _Footer(history: _history, onMore: widget.onMore, compact: compact),
            ],
          );
        },
      ),
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.title, required this.status, required this.action, required this.scene, required this.liveSince, required this.hasTraffic, required this.compact});

  final String title;
  final String status;
  final Widget? action;
  final _Scene scene;
  final double Function() liveSince;
  final bool Function() hasTraffic;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final badges = Wrap(
      spacing: 10,
      runSpacing: 8,
      alignment: compact ? WrapAlignment.start : WrapAlignment.end,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        AnimatedBuilder(
          animation: scene,
          builder: (context, _) {
            final live = scene.t - liveSince() < 30000;
            final label = !hasTraffic() ? t.liveWaiting : live ? t.liveNow : t.liveReplay;
            final color = live ? palette.success : palette.textMuted;
            final blink = live ? 0.35 + 0.65 * (0.5 + 0.5 * math.sin(scene.t / 260)) : 1.0;
            return Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
              decoration: BoxDecoration(borderRadius: BorderRadius.circular(999), border: Border.all(color: palette.borderStrong)),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Opacity(opacity: blink, child: Container(width: 7, height: 7, decoration: BoxDecoration(color: color, shape: BoxShape.circle))),
                  const SizedBox(width: 7),
                  Text(label, style: TextStyle(fontSize: 12.5, color: palette.textSoft)),
                ],
              ),
            );
          },
        ),
        ?action,
      ],
    );
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(title, style: displayTitleStyle(palette, size: compact ? 22 : 28)),
        if (status.isNotEmpty) ...<Widget>[
          const SizedBox(height: 4),
          Text(status, style: TextStyle(color: palette.textSoft, height: 1.4)),
        ],
      ],
    );
    // On a phone the badges go under the greeting instead of squeezing it.
    if (compact) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: <Widget>[heading, const SizedBox(height: 10), badges]),
      );
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 22, 20, 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Expanded(child: heading),
          const SizedBox(width: 12),
          badges,
        ],
      ),
    );
  }
}

class _Footer extends StatelessWidget {
  const _Footer({required this.history, required this.onMore, required this.compact});

  final List<RequestLog> history;
  final VoidCallback? onMore;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final mono = GoogleFonts.geistMono(fontSize: 11.5, color: palette.textMuted);
    final ok = history.where((log) => log.success).toList();
    final free = ok.where((log) => log.isFree).length;
    final caught = history.where((log) => log.attempts > 1 && log.success).length;
    final latencies = ok.map((log) => log.latencyMs).whereType<int>().toList();
    final avg = latencies.isEmpty ? null : latencies.reduce((a, b) => a + b) ~/ latencies.length;
    Widget legend(Color color, String label) => Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Container(
          width: 8,
          height: 8,
          decoration: BoxDecoration(color: color, shape: BoxShape.circle),
        ),
        const SizedBox(width: 6),
        Text(label, style: mono),
      ],
    );
    return Padding(
      padding: EdgeInsets.fromLTRB(compact ? 16 : 24, 4, compact ? 8 : 16, compact ? 8 : 10),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Wrap(
              spacing: 16,
              runSpacing: 6,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                if (history.isNotEmpty) Text(t.liveSummary('${history.length}', ok.isEmpty ? '0' : '${(free * 100 / ok.length).round()}', '$caught', avg == null ? '—' : '$avg'), style: mono),
                if (!compact) ...<Widget>[legend(palette.accent, t.liveLegendRequest), legend(palette.text, t.liveLegendAnswer), legend(palette.error, t.liveLegendFailure)],
              ],
            ),
          ),
          if (onMore != null) TextButton.icon(onPressed: onMore, icon: const Icon(Icons.keyboard_arrow_down_rounded, size: 18), label: Text(t.liveMore)),
        ],
      ),
    );
  }
}

/// The animated part: the painter underneath and the app and connection
/// cards on top, both following the scene every frame.
class _Stage extends StatelessWidget {
  const _Stage({required this.scene, required this.layout, required this.apps, required this.providers, required this.appRequests, required this.lastModel, required this.empty, required this.loaded});

  final _Scene scene;
  final _Layout layout;
  final List<String> apps;
  final List<String> providers;
  final Map<String, int> appRequests;
  final Map<int, String> lastModel;
  final bool empty;
  final bool loaded;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    // Placeholders only once there is really nothing, never while loading.
    final appNames = apps.isEmpty ? (loaded ? <String>[t.liveYourApp] : <String>[]) : apps;
    final providerNames = providers.isEmpty ? (loaded ? <String>[t.liveAddProvider] : <String>[]) : providers;
    return RepaintBoundary(
      child: AnimatedBuilder(
        animation: scene,
        builder: (context, _) {
          final s = scene;
          final thinking = s.thinking;
          final routing = s.packets.any((p) => p.phase == 'out' || p.phase == 'work');
          final coreLabel = empty
              ? t.liveWaiting
              : thinking > 0
              ? t.liveScoring('$thinking')
              : routing
              ? t.liveRouting
              : t.liveListening;
          return Stack(
            clipBehavior: Clip.none,
            children: <Widget>[
              Positioned.fill(
                child: CustomPaint(
                  painter: _TrafficPainter(scene: s, layout: layout, palette: palette),
                ),
              ),
              for (var i = 0; i < layout.appPos.length && i < appNames.length; i++)
                _node(
                  context,
                  palette,
                  center: layout.appPos[i],
                  title: appNames[i],
                  subtitle: apps.isEmpty ? '' : t.liveRequests('${appRequests[appNames[i]] ?? 0}'),
                  glow: i < s.appFlash.length ? (1 - (s.t - s.appFlash[i]) / 700).clamp(0.0, 1.0) : 0,
                  dashed: apps.isEmpty,
                  width: layout.appW,
                ),
              for (var j = 0; j < layout.provPos.length && j < providerNames.length; j++)
                _node(
                  context,
                  palette,
                  center: layout.provPos[j],
                  title: providerNames[j],
                  subtitle: providers.isEmpty ? '' : (lastModel[j] ?? ''),
                  glow: j < s.provBusy.length && s.provBusy[j] > 0 ? 0.7 : 0,
                  fail: j < s.provFail.length ? (1 - (s.t - s.provFail[j]) / 1300).clamp(0.0, 1.0) : 0,
                  shakeAt: j < s.provFail.length ? s.t - s.provFail[j] : 1e9,
                  dashed: providers.isEmpty,
                  width: layout.provW,
                ),
              // The router's own label inside the disk.
              Positioned(
                left: layout.core.dx - layout.radius,
                top: layout.core.dy - layout.radius,
                width: layout.radius * 2,
                height: layout.radius * 2,
                child: IgnorePointer(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: <Widget>[
                      Text(
                        'NeoRouter',
                        style: TextStyle(fontWeight: FontWeight.w700, fontSize: layout.radius > 50 ? 13 : 11, color: palette.text),
                      ),
                      const SizedBox(height: 2),
                      Text(t.liveInFlight('${s.packets.length}'), style: GoogleFonts.geistMono(fontSize: 10, color: palette.textMuted)),
                    ],
                  ),
                ),
              ),
              Positioned(
                left: 0,
                right: 0,
                top: layout.core.dy + layout.orbit + (layout.vertical ? 4 : 14),
                child: IgnorePointer(
                  child: Center(
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                      decoration: BoxDecoration(
                        color: palette.surface.withValues(alpha: 0.92),
                        borderRadius: BorderRadius.circular(999),
                        border: Border.all(color: palette.border),
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: <Widget>[
                          Container(
                            width: 7,
                            height: 7,
                            decoration: BoxDecoration(color: thinking > 0 ? palette.accent : palette.success, shape: BoxShape.circle),
                          ),
                          const SizedBox(width: 7),
                          Text(coreLabel, style: GoogleFonts.geistMono(fontSize: 11.5, color: palette.textSoft)),
                        ],
                      ),
                    ),
                  ),
                ),
              ),
              for (final chip in s.chips)
                if (s.t >= chip.t0) _chip(palette, chip, s.t),
            ],
          );
        },
      ),
    );
  }

  Widget _chip(NeoRouterPalette palette, _Chip chip, double now) {
    final a = ((now - chip.t0) / 1700).clamp(0.0, 1.0).toDouble();
    final opacity = a < 0.15 ? a / 0.15 : 1 - math.max(0.0, (a - 0.6) / 0.4);
    final top = layout.core.dy - layout.orbit - 26 - a * 46;
    return Positioned(
      left: layout.core.dx + chip.dx - 60,
      width: 120,
      top: top,
      child: IgnorePointer(
        child: Opacity(
          opacity: opacity.clamp(0.0, 1.0).toDouble(),
          child: Center(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 3),
              decoration: BoxDecoration(
                color: palette.scaffold.withValues(alpha: 0.9),
                borderRadius: BorderRadius.circular(999),
                border: Border.all(color: palette.accent.withValues(alpha: 0.5)),
              ),
              child: Text(chip.text, style: GoogleFonts.geistMono(fontSize: 11, color: palette.text)),
            ),
          ),
        ),
      ),
    );
  }

  Widget _node(
    BuildContext context,
    NeoRouterPalette palette, {
    required Offset center,
    required String title,
    required String subtitle,
    double glow = 0,
    double fail = 0,
    double shakeAt = 1e9,
    bool dashed = false,
    required double width,
  }) {
    final shake = fail > 0 ? math.sin(shakeAt / 28) * 4 * fail : 0.0;
    final border = fail > 0
        ? Color.lerp(palette.border, palette.error, 0.4 + fail * 0.6)!
        : glow > 0
        ? Color.lerp(palette.border, palette.accent, 0.3 + glow * 0.7)!
        : palette.border;
    final shadow = fail > 0 ? palette.error.withValues(alpha: fail * 0.45) : palette.accent.withValues(alpha: glow * 0.3);
    return Positioned(
      left: center.dx - width / 2 + shake,
      top: center.dy - layout.nodeH / 2,
      width: width,
      height: layout.nodeH,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: palette.surfaceRaised,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: dashed ? palette.borderStrong : border, width: dashed ? 1 : 1.2),
          boxShadow: <BoxShadow>[if (glow > 0 || fail > 0) BoxShadow(color: shadow, blurRadius: 18)],
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: dashed ? palette.textMuted : palette.text),
            ),
            if (subtitle.isNotEmpty)
              Text(
                fail > 0.05 ? 'failed · trying next' : subtitle,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: GoogleFonts.geistMono(fontSize: 10.5, color: fail > 0.05 ? palette.error : palette.textMuted),
              ),
          ],
        ),
      ),
    );
  }
}

class _TrafficPainter extends CustomPainter {
  _TrafficPainter({required this.scene, required this.layout, required this.palette});

  final _Scene scene;
  final _Layout layout;
  final NeoRouterPalette palette;

  @override
  void paint(Canvas canvas, Size size) {
    final s = scene;
    _grid(canvas, size);

    // Routes: faint always, gold while in use.
    final base = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2
      ..color = palette.borderStrong;
    final hot = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2;
    for (var i = 0; i < layout.appPos.length; i++) {
      final path = _path(layout.appCurve(i));
      canvas.drawPath(path, base);
      final glow = i < s.appGlow.length ? s.appGlow[i] : 0.0;
      if (glow > 0.01) canvas.drawPath(path, hot..color = palette.accent.withValues(alpha: glow * 0.85));
    }
    for (var j = 0; j < layout.provPos.length; j++) {
      final path = _path(layout.provCurve(j));
      canvas.drawPath(path, base);
      final glow = j < s.provGlow.length ? s.provGlow[j] : 0.0;
      if (glow > 0.01) canvas.drawPath(path, hot..color = palette.accent.withValues(alpha: glow * 0.85));
    }

    _core(canvas);
    for (final p in s.packets) {
      _packet(canvas, p);
    }
  }

  void _grid(Canvas canvas, Size size) {
    const gap = 24.0;
    final drift = (scene.t / 120) % gap;
    final dots = <Offset>[];
    for (var x = -gap + drift; x < size.width + gap; x += gap) {
      for (var y = -gap + drift / 2; y < size.height + gap; y += gap) {
        dots.add(Offset(x, y));
      }
    }
    canvas.drawPoints(
      PointMode.points,
      dots,
      Paint()
        ..color = palette.border
        ..strokeWidth = 1.6
        ..strokeCap = StrokeCap.round,
    );
  }

  Path _path(List<Offset> c) => Path()
    ..moveTo(c[0].dx, c[0].dy)
    ..cubicTo(c[1].dx, c[1].dy, c[2].dx, c[2].dy, c[3].dx, c[3].dy);

  void _core(Canvas canvas) {
    final s = scene;
    final c = layout.core;
    final r = layout.radius;
    final thinking = s.thinking;

    for (final start in s.pulses) {
      final a = ((s.t - start) / 1500).clamp(0.0, 1.0);
      canvas.drawCircle(
        c,
        r * (1 + a * 1.6),
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1
          ..color = palette.accent.withValues(alpha: 0.5 * (1 - a)),
      );
    }

    final glow = (0.16 + thinking * 0.1).clamp(0.16, 0.6);
    canvas.drawCircle(
      c,
      r * 1.05,
      Paint()
        ..color = palette.accent.withValues(alpha: glow)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 16 + thinking * 5.0),
    );

    // Outer ring with a slow satellite.
    final outer = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1
      ..color = palette.border;
    canvas.drawCircle(c, r * 1.62, outer);
    final sat = s.t / 5000 * math.pi * 2;
    canvas.drawCircle(c + Offset(math.cos(-sat), math.sin(-sat)) * r * 1.62, 3, Paint()..color = palette.accent.withValues(alpha: 0.6));

    // Dashed middle ring, turning the other way.
    final dash = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2
      ..color = palette.accent.withValues(alpha: 0.4);
    final turn = s.t / 9000 * math.pi * 2;
    const segments = 18;
    for (var k = 0; k < segments; k++) {
      final start = turn + k * (math.pi * 2 / segments);
      canvas.drawArc(Rect.fromCircle(center: c, radius: r * 1.25), start, math.pi * 2 / segments * 0.55, false, dash);
    }
    canvas.drawCircle(c + Offset(math.cos(turn), math.sin(turn)) * r * 1.25, 3.5, Paint()..color = palette.accent);

    // Spinner while something is being scored.
    if (thinking > 0) {
      final spin = s.t / 1100 * math.pi * 2;
      canvas.drawArc(
        Rect.fromCircle(center: c, radius: r * 1.08),
        spin,
        math.pi * 0.6,
        false,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.4
          ..strokeCap = StrokeCap.round
          ..color = palette.accent,
      );
    }

    canvas.drawCircle(c, r, Paint()..color = palette.surfaceRaised);
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1.2
        ..color = palette.accent.withValues(alpha: 0.5),
    );
  }

  Offset? _place(_Packet p, double u) {
    final e = _ease(u);
    switch (p.phase) {
      case 'in':
        return p.app < layout.appPos.length ? _bez(layout.appCurve(p.app), e) : null;
      case 'think':
        return layout.onOrbit(layout.inAngle + u.clamp(0.0, 1.0) * 3 * math.pi);
      case 'rethink':
        return layout.onOrbit(layout.outAngle + u.clamp(0.0, 1.0) * 2 * math.pi);
      case 'out':
        return p.provider >= 0 && p.provider < layout.provPos.length ? _bez(layout.provCurve(p.provider), e) : null;
      case 'bounce':
      case 'back':
        return p.provider >= 0 && p.provider < layout.provPos.length ? _bez(layout.provCurve(p.provider), 1 - e) : null;
      case 'home':
      case 'fail':
        return p.app < layout.appPos.length ? _bez(layout.appCurve(p.app), 1 - e) : null;
    }
    return null;
  }

  void _packet(Canvas canvas, _Packet p) {
    if (p.phase == 'work' || p.phase == 'done') return;
    final u = (scene.t - p.t0) / p.dur;
    final answer = p.phase == 'back' || p.phase == 'home';
    final failing = p.phase == 'bounce' || p.phase == 'fail';
    final color = failing
        ? palette.error
        : answer
        ? palette.text
        : palette.accent;
    final orbit = p.phase == 'think' || p.phase == 'rethink';
    final head = p.live ? 8.5 : 7.0;
    for (var k = 3; k >= 0; k--) {
      final pos = _place(p, u - k * (orbit ? 0.05 : 0.045));
      if (pos == null) continue;
      final size = math.max(2.5, head - k * 1.8);
      if (k == 0) {
        canvas.drawCircle(
          pos,
          size * 1.4,
          Paint()
            ..color = color.withValues(alpha: 0.45)
            ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 5),
        );
      }
      canvas.drawCircle(pos, size / 2 + (k == 0 ? 0.6 : 0), Paint()..color = color.withValues(alpha: k == 0 ? 1 : 0.5 - k * 0.12));
    }
  }

  @override
  bool shouldRepaint(_TrafficPainter oldDelegate) => true;
}
