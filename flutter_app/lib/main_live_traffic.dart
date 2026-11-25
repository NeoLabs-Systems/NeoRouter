import 'dart:async';
import 'dart:math' as math;
import 'dart:ui' show PointMode;

import 'package:flutter/foundation.dart' show listEquals;
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_router_mascot.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// The home screen's hero: the user's own traffic flowing through the
/// router. Requests from the request log travel from the key that sent them
/// into the router, circle it while it decides, fly to the provider that
/// answered (bouncing back from the ones that failed first) and return as
/// answers. Only real traffic plays, as it arrives; with none, the view
/// stays quiet and says so.
class LiveTrafficHero extends StatefulWidget {
  const LiveTrafficHero({super.key, required this.controller, required this.accounts, required this.keys, required this.ready, required this.title, required this.status, this.action, this.onMore, this.onStatusTap, this.note});

  final NeoRouterController controller;

  /// Connections and keys have arrived; until then their cards are unknown.
  final bool ready;
  final List<ProviderAccount> accounts;
  final List<RouterKey> keys;
  final String title;
  final String status;

  /// A small extra line under the status (e.g. that limits are being spread).
  final String? note;
  final Widget? action;
  final VoidCallback? onMore;

  /// Set when the status line reports a problem: opens the details.
  final VoidCallback? onStatusTap;

  @override
  State<LiveTrafficHero> createState() => _LiveTrafficHeroState();
}

// How long each leg of a request's trip takes on screen, in ms.
const double _inMs = 950, _thinkMs = 750, _outMs = 620, _bounceMs = 520, _rethinkMs = 420, _backMs = 600, _homeMs = 820;

class _Attempt {
  _Attempt(this.provider);
  int provider;
  bool failed = false;
}

/// One live request, moved along by the router's events: it flies in, waits
/// at the router until a provider attempt starts, waits inside that provider
/// until it answers or fails, and flies home once the request is done.
class _Packet {
  _Packet({required this.id, required this.app, required this.label});

  final String id;
  int app;
  final String label;
  final List<_Attempt> attempts = <_Attempt>[];
  bool finished = false;
  bool success = false;

  String phase = 'in';
  double t0 = 0;
  double dur = _inMs;
  int attempt = 0;

  bool get hasAttempt => attempt < attempts.length;
  int get provider => hasAttempt ? attempts[attempt].provider : -1;
}

/// A short-lived spark from a burst. Where it starts is resolved by the
/// painter from [anchor] ('core', 'app' or 'prov' with [index]), so sparks
/// follow cards that move.
class _Spark {
  _Spark(this.anchor, this.index, this.t0, this.angle, this.speed, this.tone, this.life);
  final String anchor;
  final int index;
  final double t0;
  final double angle;
  final double speed;
  // 0: request (gold), 1: failure (red), 2: answer (ink), 3: pool.
  final int tone;
  final double life;
}

class _Chip {
  _Chip(this.text, this.t0, this.dx);
  final bool waiting = false;
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
  final List<_Spark> sparks = <_Spark>[];

  /// Expanding rings where something hit: (anchor, index, start, tone, strength).
  final List<(String, int, double, int, double)> shocks = <(String, int, double, int, double)>[];

  /// How hard the router is working, 0 (calm) to 1 (overloaded), eased.
  double heat = 0;

  /// How much of the recent trouble was failures, 0–1, eased.
  double strain = 0;

  /// Ring rotation, accumulated so a change of speed never jumps.
  double spin = 0;

  // When a card was last hit by an arriving request, for its punch.
  List<double> provHit = <double>[];
  List<double> appHit = <double>[];

  void shock(String anchor, int index, {required int tone, double strength = 1}) {
    if (shocks.length < 40) shocks.add((anchor, index, t, tone, strength));
  }

  /// A small burst of sparks, fanned around [direction] (radians).
  void burst(String anchor, int index, {required int tone, double direction = 0, double spread = math.pi, int count = 7}) {
    if (sparks.length > 220) return;
    for (var k = 0; k < count; k++) {
      final angle = direction + (random.nextDouble() - 0.5) * spread;
      sparks.add(_Spark(anchor, index, t, angle, 0.04 + random.nextDouble() * 0.06 * (1 + heat), tone, 320 + random.nextDouble() * 260));
    }
  }
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
    if (appHit.length != apps) appHit = List<double>.filled(apps, -1e9);
    if (provHit.length != providers) provHit = List<double>.filled(providers, -1e9);
    if (provGlow.length != providers) {
      provGlow = List<double>.filled(providers, 0);
      provFail = List<double>.filled(providers, -1e9);
      provBusy = List<int>.filled(providers, 0);
    }
  }

  /// The cards were rearranged: moves everything in flight (and each card's
  /// glow) to where its card is now. map[old] is the new index, or -1.
  void remap(List<int> appMap, List<int> provMap, int apps, int providers) {
    int to(List<int> map, int index) => index >= 0 && index < map.length ? map[index] : -1;
    List<T> moved<T>(List<T> old, List<int> map, int length, T empty) {
      final next = List<T>.filled(length, empty);
      for (var i = 0; i < old.length; i++) {
        final j = to(map, i);
        if (j >= 0 && j < length) next[j] = old[i];
      }
      return next;
    }

    appGlow = moved(appGlow, appMap, apps, 0.0);
    appFlash = moved(appFlash, appMap, apps, -1e9);
    provGlow = moved(provGlow, provMap, providers, 0.0);
    provFail = moved(provFail, provMap, providers, -1e9);
    provBusy = moved(provBusy, provMap, providers, 0);
    for (final p in packets) {
      p.app = math.max(0, to(appMap, p.app));
      for (final attempt in p.attempts) {
        attempt.provider = to(provMap, attempt.provider);
      }
    }
    // A request whose card is gone has nowhere to fly.
    // A request at a card that is gone heads back to the router.
    for (final p in packets) {
      if ((p.phase == 'out' || p.phase == 'work') && p.provider < 0) _go(p, 'think', _thinkMs);
    }
  }

  int get thinking => packets.where((p) => p.phase == 'think' || p.phase == 'rethink').length;
  int get waiting => packets.where((p) => p.phase == 'work').length;

  /// The chip shown when a request starts waiting (localized by the view).
  String waitingLabel = 'waiting';

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
    sparks.removeWhere((spark) => t - spark.t0 > spark.life);
    shocks.removeWhere((shock) => t - shock.$3 > 900);
    // Heat follows how many requests are under way (about 12 is a lot).
    final target = (packets.length / 12).clamp(0.0, 1.0);
    heat += (target - heat) * math.min(1, dt / 900);
    strain = math.max(0, strain - dt / 6000);
    spin += dt * (1 + heat * 2.5);
    notifyListeners();
  }

  void _go(_Packet p, String phase, double dur) {
    p.phase = phase;
    p.t0 = t;
    p.dur = dur;
  }

  // A failure lands hard: a red burst and ring, and the router feels it.
  void _impactFail(int provider) {
    burst('prov', provider, tone: 1, direction: math.pi, count: 9, spread: math.pi * 1.3);
    shock('prov', provider, tone: 1, strength: 1.3);
    strain = math.min(1, strain + 0.25);
  }

  // Waiting phases ('think' at the router, 'work' at a provider) repeat
  // until the event they wait for has arrived.
  void _next(_Packet p) {
    void leaveProvider() {
      if (p.provider >= 0 && p.provider < provBusy.length) provBusy[p.provider] = math.max(0, provBusy[p.provider] - 1);
    }

    switch (p.phase) {
      case 'in':
        _go(p, 'think', _thinkMs);
        pulses.add(t);
        burst('in', p.app, tone: 0, count: 5);
        shock('core', 0, tone: 0, strength: 0.5 + heat * 0.6);
        if (chips.isEmpty || t - chips.last.t0 > 450) chips.add(_Chip(p.label, t, (random.nextDouble() - 0.5) * 90));
      case 'think':
      case 'rethink':
        if (p.hasAttempt && p.provider >= 0) {
          _go(p, 'out', _outMs);
          if (p.provider < provGlow.length) provGlow[p.provider] = 1;
        } else if (p.hasAttempt) {
          // Its card is not on screen: skip that attempt.
          p.attempt += 1;
          _go(p, 'think', _thinkMs);
        } else if (p.finished) {
          _go(p, p.success ? 'home' : 'fail', _homeMs);
        } else {
          _go(p, 'think', _thinkMs);
        }
      case 'out':
        if (p.attempts[p.attempt].failed) {
          if (p.provider < provFail.length) provFail[p.provider] = t;
          _impactFail(p.provider);
          _go(p, 'bounce', _bounceMs);
          return;
        }
        burst('prov', p.provider, tone: 0, direction: math.pi, count: 5);
        shock('prov', p.provider, tone: 0, strength: 0.6);
        if (p.provider < provHit.length) provHit[p.provider] = t;
        if (p.provider < provBusy.length) provBusy[p.provider] += 1;
        _go(p, 'work', 600);
      case 'work':
        final current = p.attempts[p.attempt];
        if (current.failed || (p.finished && !p.success)) {
          leaveProvider();
          if (p.provider < provFail.length) provFail[p.provider] = t;
          _impactFail(p.provider);
          _go(p, 'bounce', _bounceMs);
        } else if (p.finished) {
          leaveProvider();
          if (p.provider < provGlow.length) provGlow[p.provider] = 1;
          _go(p, 'back', _backMs);
        } else {
          _go(p, 'work', 600);
        }
      case 'bounce':
        p.attempt += 1;
        if (p.finished && !p.hasAttempt) {
          _go(p, p.success ? 'home' : 'fail', _homeMs);
        } else {
          _go(p, p.hasAttempt ? 'rethink' : 'think', p.hasAttempt ? _rethinkMs : _thinkMs);
          pulses.add(t);
        }
      case 'back':
        _go(p, 'home', _homeMs);
        if (p.app < appGlow.length) appGlow[p.app] = 1;
      case 'home':
      case 'fail':
        burst('app', p.app, tone: p.phase == 'home' ? 2 : 1, direction: 0, count: p.phase == 'home' ? 6 : 5);
        shock('app', p.app, tone: p.phase == 'home' ? 2 : 1, strength: p.phase == 'home' ? 0.5 : 0.8);
        if (p.app < appHit.length) appHit[p.app] = t;
        p.phase = 'done';
        if (p.app < appFlash.length) appFlash[p.app] = t;
    }
  }
}

/// Where everything sits for a given size: side by side when there is room,
/// apps on top and providers below on a phone.
class _Layout {
  _Layout(this.size, int apps, int providers, {this.poolIndex}) {
    vertical = size.width < 640;
    const pad = 16.0;
    if (!vertical) {
      nodeW = (size.width * 0.16).clamp(124, 200).toDouble();
      nodeH = 50;
      radius = (math.min(size.width, size.height) * 0.1).clamp(40, 74).toDouble();
      core = Offset(size.width / 2, size.height / 2);
      appPos = _column(apps, pad + nodeW / 2, 84);
      provPos = _column(providers, size.width - pad - nodeW / 2, 72);
      // The pool card stands out of the column, nearer the router.
      if (poolIndex != null && poolIndex! < provPos.length) provPos[poolIndex!] = provPos[poolIndex!].translate(-nodeW * 0.35, 8);
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
  final int? poolIndex;
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

  // The first snapshot (cards and what is in flight) has arrived.
  bool _ready = false;
  bool _disposed = false;

  final _Scene _scene = _Scene();
  late final Ticker _ticker;
  Duration _last = Duration.zero;
  Timer? _footerPoll;
  int _seq = 0;
  // Recent finished requests, only for the footer's numbers.
  final List<RequestLog> _history = <RequestLog>[];
  double _lastLive = -1e9;

  List<String> _apps = <String>[];
  List<String> _providers = <String>[];
  // Provider keys of the cards ("*": the rest, "#pool": pool partners).
  List<String> _providerSlugs = <String>[];
  int? _poolIndex;
  // Which card each connection (account slug) belongs to.
  Map<String, String> _slugToProvider = <String, String>{};
  final Map<String, int> _appRequests = <String, int>{};
  // The model each provider card answered with last, by card key.
  final Map<String, String> _lastModel = <String, String>{};
  final Map<String, _Packet> _byId = <String, _Packet>{};

  static const String _poolSlug = '#pool';
  String get _otherLabel => AppL10n.of(context).liveOther;

  @override
  void initState() {
    super.initState();
    _ticker = createTicker(_tick)..start();
    _listen();
    _fetchFooter();
    _footerPoll = Timer.periodic(const Duration(seconds: 30), (_) => _fetchFooter());
  }

  @override
  void dispose() {
    _disposed = true;
    _footerPoll?.cancel();
    _ticker.dispose();
    _scene.dispose();
    super.dispose();
  }

  // Snapshot first, then every event as it happens (long polling).
  Future<void> _listen() async {
    while (!_disposed) {
      try {
        final data = await widget.controller.api.routingLive(since: _seq, waitMs: _seq == 0 ? 0 : 20000);
        if (_disposed || !mounted) return;
        if (data['snapshot'] == true) {
          _applySnapshot(data);
        } else {
          for (final event in (data['events'] as List).cast<Map>()) {
            _apply(event);
          }
        }
        _seq = (data['seq'] as num).toInt();
      } catch (_) {
        if (_disposed) return;
        await Future<void>.delayed(const Duration(seconds: 3));
      }
    }
  }

  Future<void> _fetchFooter() async {
    try {
      final logs = await widget.controller.api.logs(limit: 40);
      if (!mounted) return;
      setState(() => _history
        ..clear()
        ..addAll(logs));
    } catch (_) {}
  }

  void _applySnapshot(Map data) {
    final cards = (data['cards'] as Map?) ?? const <String, Object?>{};
    final keys = ((cards['keys'] as List?) ?? const <Object>[]).cast<Map>();
    final providers = ((cards['providers'] as List?) ?? const <Object>[]).cast<Map>();
    final inFlight = ((data['inFlight'] as List?) ?? const <Object>[]).cast<Map>();
    final wide = MediaQuery.maybeSizeOf(context)?.width ?? 1200;
    final maxApps = wide < 640 ? 4 : 6;
    final keyNames = keys.map((key) => '${key['name']}').toList();
    final oldApps = _apps, oldSlugs = _providerSlugs;
    final apps = keyNames.length > maxApps ? <String>[...keyNames.take(maxApps - 1), _otherLabel] : keyNames;
    final hasPoolTraffic = inFlight.any((item) => item['key'] == _poolSlug);
    _apps = <String>[if (apps.isEmpty && !hasPoolTraffic) AppL10n.of(context).liveYourApp, ...apps, if (hasPoolTraffic || (widget.controller.pools?.inPool ?? false)) AppL10n.of(context).filterPool];
    _appRequests.clear();
    for (final key in keys) {
      _appRequests[_appFor('${key['name']}')] = (_appRequests[_appFor('${key['name']}')] ?? 0) + ((key['requests'] as num?)?.toInt() ?? 0);
    }
    const maxProviders = 8;
    final shown = providers.length > maxProviders ? providers.take(maxProviders - 1).toList() : providers;
    _slugToProvider = <String, String>{
      for (final card in providers)
        for (final slug in (card['slugs'] as List).cast<String>()) slug: shown.contains(card) ? '${card['key']}' : '*',
    };
    final inPool = widget.controller.pools?.inPool ?? false;
    _providerSlugs = <String>[for (final card in shown) '${card['key']}', if (providers.length > maxProviders) '*', if (inPool) _poolSlug];
    _providers = <String>[
      for (final card in shown) '${card['name']}',
      if (providers.length > maxProviders) _otherLabel,
      if (inPool) AppL10n.of(context).filterPool,
    ];
    if (_providers.isEmpty) _providers = <String>[AppL10n.of(context).liveAddProvider];
    _poolIndex = inPool ? _providerSlugs.length - 1 : null;
    if (!listEquals(oldApps, _apps) || !listEquals(oldSlugs, _providerSlugs)) {
      _scene.remap(<int>[for (final name in oldApps) _apps.indexOf(name)], <int>[for (final slug in oldSlugs) _providerSlugs.indexOf(slug)], math.max(1, _apps.length), math.max(1, _providers.length));
    }
    _scene.resize(math.max(1, _apps.length), math.max(1, _providers.length));
    // Requests already under way start where they are: at the router, or
    // waiting inside the provider they went to.
    _scene.packets.clear();
    _byId.clear();
    for (final item in inFlight) {
      final packet = _newPacket('${item['id']}', '${item['key']}', '${item['task'] ?? 'chat'}');
      for (final attempt in ((item['attempts'] as List?) ?? const <Object>[]).cast<Map>()) {
        packet.attempts.add(_Attempt(_providerIndex('${attempt['slug']}'))..failed = attempt['state'] == 'failed');
      }
      while (packet.hasAttempt && packet.attempts[packet.attempt].failed) {
        packet.attempt += 1;
      }
      _scene.add(packet);
      if (packet.hasAttempt && packet.provider >= 0) {
        packet
          ..phase = 'work'
          ..dur = 600
          ..t0 = _scene.t - _scene.random.nextDouble() * 600;
        if (packet.provider < _scene.provBusy.length) _scene.provBusy[packet.provider] += 1;
      } else {
        packet
          ..phase = 'think'
          ..dur = _thinkMs
          ..t0 = _scene.t - _scene.random.nextDouble() * _thinkMs;
      }
    }
    setState(() => _ready = true);
  }

  void _apply(Map event) {
    final id = '${event['id']}';
    switch (event['type']) {
      case 'start':
        final packet = _newPacket(id, '${event['key']}', '${event['task'] ?? 'chat'}');
        _appRequests[_apps[packet.app]] = (_appRequests[_apps[packet.app]] ?? 0) + 1;
        _scene.add(packet);
        _lastLive = _scene.t;
      case 'attempt':
        _byId[id]?.attempts.add(_Attempt(_providerIndex('${event['slug']}')));
      case 'fail':
        final packet = _byId[id];
        final index = _providerIndex('${event['slug']}');
        for (final attempt in packet?.attempts ?? const <_Attempt>[]) {
          if (attempt.provider == index && !attempt.failed) {
            attempt.failed = true;
            break;
          }
        }
      case 'done':
        final packet = _byId.remove(id);
        if (packet == null) return;
        packet
          ..finished = true
          ..success = event['success'] == true;
        final model = '${event['model'] ?? ''}';
        if (packet.success && model.contains('/')) {
          final card = _providerIndex(model.split('/').first);
          if (card >= 0 && card < _providerSlugs.length) _lastModel[_providerSlugs[card]] = model.substring(model.indexOf('/') + 1);
        }
        _lastLive = _scene.t;
    }
  }

  _Packet _newPacket(String id, String key, String task) {
    final packet = _Packet(id: id, app: math.max(0, _apps.indexOf(_appFor(key))), label: task.replaceAll('_', ' '));
    _byId[id] = packet;
    return packet;
  }

  String _appFor(String key) {
    if (key == _poolSlug) return AppL10n.of(context).filterPool;
    if (_apps.contains(key)) return key;
    if (_apps.contains(_otherLabel)) return _otherLabel;
    return _apps.isEmpty ? '' : _apps.first;
  }

  int _providerIndex(String slug) {
    // Partner connections are named pool-<partner>; they all go to the pool card.
    if (slug.startsWith('pool-') && _poolIndex != null) return _poolIndex!;
    final index = _providerSlugs.indexOf(_slugToProvider[slug] ?? slug);
    return index >= 0 ? index : _providerSlugs.indexOf('*');
  }

  double get _speed => MediaQuery.maybeDisableAnimationsOf(context) == true ? 0.5 : 1;

  void _tick(Duration elapsed) {
    final dt = math.min(100.0, (elapsed - _last).inMicroseconds / 1000);
    _last = elapsed;
    _scene.advance(dt * _speed);
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    _scene.waitingLabel = AppL10n.of(context).liveWaitingChip;
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
              _Header(title: widget.title, status: widget.status, note: widget.note, onStatusTap: widget.onStatusTap, action: widget.action, scene: _scene, liveSince: () => _lastLive, hasTraffic: () => _history.isNotEmpty, loaded: () => _ready, compact: compact),
              Expanded(
                child: LayoutBuilder(
                  builder: (context, stage) {
                    // While loading, skeleton slots stand in for keys and connections.
                    final apps = _ready ? math.max(1, _apps.length) : (compact ? 3 : 4);
                    final providers = _ready ? math.max(1, _providers.length) : (compact ? 4 : 5);
                    final layout = _Layout(Size(stage.maxWidth, stage.maxHeight), apps, providers, poolIndex: _poolIndex);
                    final lastModels = <int, String>{
                      for (var j = 0; j < _providerSlugs.length; j++)
                        if (_lastModel[_providerSlugs[j]] != null) j: _lastModel[_providerSlugs[j]]!,
                    };
                    return _Stage(controller: widget.controller, scene: _scene, layout: layout, apps: _ready ? _apps : const <String>[], providers: _ready ? _providers : const <String>[], appRequests: _appRequests, lastModel: lastModels, empty: _history.isEmpty, loaded: _ready, poolIndex: _poolIndex);
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
  const _Header({required this.title, required this.status, this.note, this.onStatusTap, required this.action, required this.scene, required this.liveSince, required this.hasTraffic, required this.loaded, required this.compact});

  final String title;
  final String status;
  final String? note;
  final VoidCallback? onStatusTap;
  final Widget? action;
  final _Scene scene;
  final double Function() liveSince;
  final bool Function() loaded;
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
            final label = !loaded() ? t.liveLoading : !hasTraffic() ? t.liveWaiting : live ? t.liveNow : t.liveQuiet;
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
          if (onStatusTap == null)
            Text(status, style: TextStyle(color: palette.textSoft, height: 1.4))
          else
            // A problem: the line itself opens which connections and why.
            InkWell(
              onTap: onStatusTap,
              borderRadius: BorderRadius.circular(6),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 2),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    Container(width: 7, height: 7, decoration: BoxDecoration(color: palette.warning, shape: BoxShape.circle)),
                    const SizedBox(width: 8),
                    Flexible(
                      child: Text(
                        status,
                        style: TextStyle(color: palette.text, height: 1.4, decoration: TextDecoration.underline, decorationColor: palette.textMuted),
                      ),
                    ),
                    const SizedBox(width: 4),
                    Icon(Icons.chevron_right_rounded, size: 18, color: palette.textSoft),
                  ],
                ),
              ),
            ),
        ],
        if (note != null) ...<Widget>[
          const SizedBox(height: 4),
          Row(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Padding(padding: const EdgeInsets.only(top: 2), child: Icon(Icons.schedule_rounded, size: 13, color: palette.textMuted)),
              const SizedBox(width: 6),
              Flexible(child: Text(note!, style: TextStyle(fontSize: 12.5, color: palette.textMuted, height: 1.4))),
            ],
          ),
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
  const _Stage({required this.controller, required this.scene, required this.layout, required this.apps, required this.providers, required this.appRequests, required this.lastModel, required this.empty, required this.loaded, this.poolIndex});

  final NeoRouterController controller;
  final _Scene scene;
  final _Layout layout;
  final List<String> apps;
  final List<String> providers;
  final Map<String, int> appRequests;
  final Map<int, String> lastModel;
  final bool empty;
  final bool loaded;
  final int? poolIndex;

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
          final coreLabel = !loaded
              ? t.liveLoadingTraffic
              : empty
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
                  painter: _TrafficPainter(scene: s, layout: layout, palette: palette, loading: !loaded, poolIndex: poolIndex),
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
                  hitAt: i < s.appHit.length ? s.t - s.appHit[i] : 1e9,
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
                  hitAt: j < s.provHit.length ? s.t - s.provHit[j] : 1e9,
                  dashed: providers.isEmpty,
                  width: layout.provW,
                  tint: j == poolIndex ? palette.pool : null,
                ),
              if (!loaded)
                for (var i = 0; i < layout.appPos.length; i++) _skeleton(palette, layout.appPos[i], layout.appW, s.t, i),
              if (!loaded)
                for (var j = 0; j < layout.provPos.length; j++) _skeleton(palette, layout.provPos[j], layout.provW, s.t, j + 3),
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
                      RouterMascot(controller: controller, size: layout.radius * 0.95, tooltip: false),
                      const SizedBox(height: 4),
                      Text(t.liveInFlight('${s.packets.where((p) => !p.finished).length}'), style: GoogleFonts.geistMono(fontSize: 10, color: palette.textMuted)),
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
              // Requests waiting on a provider sit inside its card as dots.
              for (var j = 0; j < layout.provPos.length && j < providerNames.length && j < s.provBusy.length; j++)
                if (s.provBusy[j] > 0 && providers.isNotEmpty) _waiting(palette, layout.provPos[j], layout.provW, s.provBusy[j], s.t),
              for (final chip in s.chips)
                if (s.t >= chip.t0) _chip(palette, chip, s.t),
            ],
          );
        },
      ),
    );
  }

  // Up to five pulsing dots in the card's top-right corner, then "+n".
  Widget _waiting(NeoRouterPalette palette, Offset center, double width, int count, double now) {
    final shown = math.min(count, 5);
    return Positioned(
      left: center.dx + width / 2 - 14 - shown * 9 - (count > shown ? 22 : 0),
      top: center.dy - layout.nodeH / 2 + 10,
      child: IgnorePointer(
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            for (var k = 0; k < shown; k++)
              Container(
                width: 6,
                height: 6,
                margin: const EdgeInsets.only(left: 3),
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: palette.accent.withValues(alpha: 0.45 + 0.55 * (0.5 + 0.5 * math.sin(now / 260 - k * 0.9))),
                ),
              ),
            if (count > shown) Padding(padding: const EdgeInsets.only(left: 4), child: Text('+${count - shown}', style: GoogleFonts.geistMono(fontSize: 10, color: palette.accent))),
          ],
        ),
      ),
    );
  }

  // A card-shaped placeholder that shimmers while the traffic loads.
  Widget _skeleton(NeoRouterPalette palette, Offset center, double width, double now, int index) {
    final shimmer = 0.5 + 0.5 * math.sin(now / 420 - index * 0.6);
    final bar = Color.lerp(palette.surfaceRaised, palette.borderStrong, 0.4 + 0.6 * shimmer)!;
    return Positioned(
      left: center.dx - width / 2,
      top: center.dy - layout.nodeH / 2,
      width: width,
      height: layout.nodeH,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: palette.surfaceRaised,
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: palette.border),
        ),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            FractionallySizedBox(
              widthFactor: 0.7,
              child: Container(height: 9, decoration: BoxDecoration(color: bar, borderRadius: BorderRadius.circular(5))),
            ),
            const SizedBox(height: 7),
            FractionallySizedBox(
              widthFactor: 0.45,
              child: Container(height: 7, decoration: BoxDecoration(color: bar.withValues(alpha: 0.6), borderRadius: BorderRadius.circular(4))),
            ),
          ],
        ),
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
                border: Border.all(color: chip.waiting ? palette.borderStrong : palette.accent.withValues(alpha: 0.5)),
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
    double hitAt = 1e9,
    bool dashed = false,
    required double width,
    // The pool card: its own colour instead of the accent.
    Color? tint,
  }) {
    final shake = fail > 0 ? math.sin(shakeAt / 28) * 6 * fail : 0.0;
    // A landing request knocks the card back a little; it springs back.
    final punch = hitAt < 320 ? math.sin(hitAt / 320 * math.pi) * (1 - hitAt / 320) : 0.0;
    final rest = tint == null ? palette.border : tint.withValues(alpha: 0.55);
    final hot = tint ?? palette.accent;
    final border = fail > 0
        ? Color.lerp(rest, palette.error, 0.4 + fail * 0.6)!
        : glow > 0
        ? Color.lerp(rest, hot, 0.3 + glow * 0.7)!
        : rest;
    final shadow = fail > 0 ? palette.error.withValues(alpha: fail * 0.45) : hot.withValues(alpha: glow * 0.3);
    return Positioned(
      left: center.dx - width / 2 + shake,
      top: center.dy - layout.nodeH / 2,
      width: width,
      height: layout.nodeH,
      child: Transform.scale(
        scale: 1 + punch * 0.07,
        child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10),
        decoration: BoxDecoration(
          color: tint == null ? palette.surfaceRaised : Color.alphaBlend(tint.withValues(alpha: 0.08), palette.surfaceRaised),
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
      )
    );
  }
}

class _TrafficPainter extends CustomPainter {
  _TrafficPainter({required this.scene, required this.layout, required this.palette, this.loading = false, this.poolIndex});

  final _Scene scene;
  final _Layout layout;
  final NeoRouterPalette palette;
  final bool loading;
  // The pool's route is drawn in the pool colour.
  final int? poolIndex;

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
      final pool = j == poolIndex;
      canvas.drawPath(path, pool ? (Paint.from(base)..color = palette.pool.withValues(alpha: 0.4)) : base);
      final glow = j < s.provGlow.length ? s.provGlow[j] : 0.0;
      if (glow > 0.01) canvas.drawPath(path, hot..color = (pool ? palette.pool : palette.accent).withValues(alpha: glow * 0.85));
    }

    if (loading) _loadingPulses(canvas);
    _core(canvas);
    for (final p in s.packets) {
      _packet(canvas, p);
    }
    _shocks(canvas);
    _sparks(canvas);
  }

  // While loading: a soft light runs along every line, in and out of the router.
  void _loadingPulses(Canvas canvas) {
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 2
      ..strokeCap = StrokeCap.round;
    final curves = <List<Offset>>[
      for (var i = 0; i < layout.appPos.length; i++) layout.appCurve(i),
      for (var j = 0; j < layout.provPos.length; j++) layout.provCurve(j),
    ];
    for (var k = 0; k < curves.length; k++) {
      final metric = _path(curves[k]).computeMetrics().first;
      final phase = ((scene.t / 1600) + k * 0.17) % 1.0;
      final head = metric.length * phase;
      final segment = metric.extractPath(math.max(0, head - 60), head);
      paint.color = palette.accent.withValues(alpha: 0.5 * math.sin(phase * math.pi));
      canvas.drawPath(segment, paint);
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

  Offset? _anchor(String anchor, int index) => switch (anchor) {
    'prov' => index >= 0 && index < layout.provPos.length ? layout.provCurve(index).last : null,
    'app' => index < layout.appPos.length ? layout.appCurve(index).first : null,
    'in' => index < layout.appPos.length ? layout.appCurve(index).last : null,
    _ => layout.core,
  };

  // A thin ring that races out from an impact and thins as it goes.
  void _shocks(Canvas canvas) {
    final s = scene;
    for (final (anchor, index, t0, tone, strength) in s.shocks) {
      final origin = _anchor(anchor, index);
      if (origin == null) continue;
      final u = ((s.t - t0) / 900).clamp(0.0, 1.0);
      final e = 1 - math.pow(1 - u, 3).toDouble();
      final base = anchor == 'core' ? layout.radius : 6.0;
      final color = switch (tone) {
        1 => palette.error,
        2 => palette.text,
        _ => palette.accent,
      };
      canvas.drawCircle(
        origin,
        base + e * 46 * strength,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = (2.6 * strength) * (1 - u) + 0.3
          ..color = color.withValues(alpha: 0.55 * (1 - u) * math.min(1, strength)),
      );
    }
  }

  // Sparks fly out, slow down and fade, with a little fall: a short streak
  // from where they were a moment ago to where they are.
  void _sparks(Canvas canvas) {
    final s = scene;
    final paint = Paint()
      ..strokeCap = StrokeCap.round
      ..style = PaintingStyle.stroke;
    for (final spark in s.sparks) {
      final Offset origin;
      switch (spark.anchor) {
        case 'in':
          if (spark.index >= layout.appPos.length) continue;
          origin = layout.appCurve(spark.index).last;
        case 'prov':
          if (spark.index < 0 || spark.index >= layout.provPos.length) continue;
          origin = layout.provCurve(spark.index).last;
        case 'app':
          if (spark.index >= layout.appPos.length) continue;
          origin = layout.appCurve(spark.index).first;
        default:
          origin = layout.core;
      }
      final age = s.t - spark.t0;
      final u = (age / spark.life).clamp(0.0, 1.0);
      Offset at(double ms) {
        // Eases out: fast at first, then it drifts.
        final travel = spark.speed * spark.life * (1 - math.pow(1 - (ms / spark.life).clamp(0.0, 1.0), 2.4));
        return origin + Offset(math.cos(spark.angle), math.sin(spark.angle)) * travel + Offset(0, 0.00004 * ms * ms);
      }
      final color = switch (spark.tone) {
        1 => palette.error,
        2 => palette.text,
        3 => palette.pool,
        _ => palette.accent,
      };
      paint
        ..strokeWidth = 1.5 * (1 - u) + 0.4
        ..color = color.withValues(alpha: (1 - u) * 0.9);
      canvas.drawLine(at(math.max(0, age - 60)), at(age), paint);
    }
  }

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

    // Hotter and redder as load and failures build up; trembles when overloaded.
    final heat = s.heat;
    final core = Color.lerp(palette.accent, palette.error, (s.strain * 0.8).clamp(0.0, 0.8))!;
    final glow = (0.16 + thinking * 0.08 + heat * 0.34).clamp(0.16, 0.7);
    final tremble = heat > 0.6 ? Offset(math.sin(s.t / 37) , math.cos(s.t / 41)) * (heat - 0.6) * 4 : Offset.zero;
    canvas.save();
    canvas.translate(tremble.dx, tremble.dy);
    canvas.drawCircle(
      c,
      r * (1.05 + heat * 0.25 + 0.03 * math.sin(s.t / (420 - heat * 220))),
      Paint()
        ..color = core.withValues(alpha: glow)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, 16 + thinking * 4.0 + heat * 18),
    );

    // Outer ring with a slow satellite.
    final outer = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1
      ..color = palette.border;
    canvas.drawCircle(c, r * 1.62, outer);
    final sat = s.spin / 5000 * math.pi * 2;
    canvas.drawCircle(c + Offset(math.cos(-sat), math.sin(-sat)) * r * 1.62, 3, Paint()..color = palette.accent.withValues(alpha: 0.6));

    // Dashed middle ring, turning the other way.
    final dash = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2
      ..color = core.withValues(alpha: 0.4 + heat * 0.3);
    final turn = s.spin / 9000 * math.pi * 2;
    const segments = 18;
    for (var k = 0; k < segments; k++) {
      final start = turn + k * (math.pi * 2 / segments);
      canvas.drawArc(Rect.fromCircle(center: c, radius: r * 1.25), start, math.pi * 2 / segments * 0.55, false, dash);
    }
    canvas.drawCircle(c + Offset(math.cos(turn), math.sin(turn)) * r * 1.25, 3.5, Paint()..color = core);

    // Spinner while something is being scored.
    if (thinking > 0 || loading) {
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
        ..color = core.withValues(alpha: 0.5 + heat * 0.4),
    );
    canvas.restore();
  }

  Offset? _place(_Packet p, double u) {
    final e = _ease(u);
    switch (p.phase) {
      case 'in':
        return p.app < layout.appPos.length ? _bez(layout.appCurve(p.app), e) : null;
      case 'queue':
        // A slow, wider lap, ending where scoring starts.
        return layout.core + (layout.onOrbit(layout.inAngle + u.clamp(0.0, 1.0) * 2 * math.pi) - layout.core) * 1.35;
      case 'think':
        // Full laps, so a request still waiting circles on without a jump.
        return layout.onOrbit(layout.inAngle + u.clamp(0.0, 1.0) * 2 * math.pi);
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
    // Smaller when it is busy, so many requests at once stay tidy.
    final head = 7.5 - scene.heat * 2.5;
    for (var k = 3; k >= 0; k--) {
      final pos = _place(p, u - k * (orbit ? 0.05 : 0.045));
      if (pos == null) continue;
      final size = math.max(2.0, head - k * 1.6);
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
