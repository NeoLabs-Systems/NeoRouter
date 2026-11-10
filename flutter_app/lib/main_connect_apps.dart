import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_picker.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';
import 'src/web_media.dart';

/// Settings → Connect your apps: pick the app, say where it runs, paste what
/// NeoRouter shows, and watch the first request arrive. NeoRouter works out
/// the address (loopback, Docker, network, internet) and explains whatever
/// stands in the way.
class ConnectAppsPage extends StatefulWidget {
  const ConnectAppsPage({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ConnectAppsPage> createState() => _ConnectAppsPageState();
}

class _ConnectAppsPageState extends State<ConnectAppsPage> {
  Map<String, dynamic>? _data;
  String? _error;
  Map<String, dynamic>? _app;
  String? _place;
  bool _placeConfirmed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final data = await widget.controller.api.connectApps();
      if (mounted) setState(() => _data = data);
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    }
  }

  List<Map<String, dynamic>> get _apps => ((_data?['apps'] as List?) ?? const <Object>[]).map((item) => Map<String, dynamic>.from(item as Map)).toList();
  List<Map<String, dynamic>> get _places => ((_data?['places'] as List?) ?? const <Object>[]).map((item) => Map<String, dynamic>.from(item as Map)).toList();

  // A NeoRouter on its own domain has one address for everyone: no place to pick.
  bool get _single => _places.length == 1;

  void _pickApp(Map<String, dynamic> app) => setState(() {
        _app = app;
        _place = _single ? '${_places.first['id']}' : app['cloudOnly'] == true ? 'internet' : '${app['runs'] ?? 'local'}';
        _placeConfirmed = _single;
      });

  void _restart() => setState(() {
        _app = null;
        _place = null;
        _placeConfirmed = false;
      });

  // 0 app · 1 where it runs · 2 set up (where it runs is skipped when there is one address).
  int get _step => _app == null ? 0 : !_placeConfirmed ? 1 : 2;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final data = _data;
    return AsyncBody(
      loading: data == null && _error == null,
      error: data == null ? _error : null,
      onRetry: _load,
      child: data == null
          ? const SizedBox()
          : Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Text(t.caIntro, style: TextStyle(color: neoRouterPaletteOf(context).textSoft, height: 1.45)),
                const SizedBox(height: 18),
                _Stepper(
                  step: _single && _step == 2 ? 1 : _step,
                  labels: <String>[t.caStepApp, if (!_single) t.caStepPlace, t.caStepSetup],
                  onTap: (step) => setState(() {
                    if (step == 0) _restart();
                    if (step == 1 && !_single) _placeConfirmed = false;
                  }),
                ),
                const SizedBox(height: 20),
                // Only the new step fades in; the old one leaves at once, so the two never overlap.
                AnimatedSwitcher(
                  duration: const Duration(milliseconds: 220),
                  layoutBuilder: (current, _) => current ?? const SizedBox(),
                  transitionBuilder: (child, animation) => FadeTransition(
                    opacity: animation,
                    child: SlideTransition(position: Tween<Offset>(begin: const Offset(0, 0.015), end: Offset.zero).animate(animation), child: child),
                  ),
                  child: switch (_step) {
                    0 => _AppGrid(key: const ValueKey<String>('apps'), apps: _apps, onPick: _pickApp),
                    1 => _PlaceStep(
                        key: ValueKey<String>('place-${_app!['id']}'),
                        controller: widget.controller,
                        app: _app!,
                        places: _places,
                        selected: _place!,
                        onSelect: (place) => setState(() => _place = place),
                        onPlaces: (places) => setState(() => _data = <String, dynamic>{...?_data, 'places': places}),
                        onBack: _restart,
                        onContinue: () => setState(() => _placeConfirmed = true),
                      ),
                    _ => _SetupStep(
                        key: ValueKey<String>('setup-${_app!['id']}-$_place'),
                        controller: widget.controller,
                        app: _app!,
                        place: _places.firstWhere((place) => place['id'] == _place),
                        onBack: () => _single ? _restart() : setState(() => _placeConfirmed = false),
                        onAnother: _restart,
                      ),
                  },
                ),
              ],
            ),
    );
  }
}

String _appName(Map<String, dynamic> app) => '${app['name']}';

String _fill(String template, Map<String, String> values) =>
    template.replaceAllMapped(RegExp(r'\{\{(\w+)\}\}'), (match) => values[match.group(1)] ?? match.group(0)!);

/// Three steps with numbers and checks; finished ones can be clicked to go back.
class _Stepper extends StatelessWidget {
  const _Stepper({required this.step, required this.labels, required this.onTap});

  final int step;
  final List<String> labels;
  final ValueChanged<int> onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      children: <Widget>[
        for (var i = 0; i < labels.length; i++) ...<Widget>[
          if (i > 0) Expanded(child: Container(height: 1, margin: const EdgeInsets.symmetric(horizontal: 10), color: i <= step ? palette.accent.withValues(alpha: 0.6) : palette.border)),
          InkWell(
            borderRadius: BorderRadius.circular(AppRadius.pill),
            onTap: i < step ? () => onTap(i) : null,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  AnimatedContainer(
                    duration: const Duration(milliseconds: 200),
                    width: 24,
                    height: 24,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      shape: BoxShape.circle,
                      color: i < step ? palette.accent : i == step ? palette.accentSoft : Colors.transparent,
                      border: Border.all(color: i <= step ? palette.accent : palette.borderStrong),
                    ),
                    child: i < step
                        ? Icon(Icons.check_rounded, size: 14, color: palette.scaffold)
                        : Text('${i + 1}', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700, color: i == step ? palette.accent : palette.textMuted)),
                  ),
                  const SizedBox(width: 8),
                  Text(labels[i], style: TextStyle(fontSize: 13, fontWeight: i == step ? FontWeight.w600 : FontWeight.w500, color: i <= step ? palette.text : palette.textMuted)),
                ],
              ),
            ),
          ),
        ],
      ],
    );
  }
}

// ---------------------------------------------------------------- step 1

class _AppGrid extends StatefulWidget {
  const _AppGrid({super.key, required this.apps, required this.onPick});

  final List<Map<String, dynamic>> apps;
  final ValueChanged<Map<String, dynamic>> onPick;

  @override
  State<_AppGrid> createState() => _AppGridState();
}

class _AppGridState extends State<_AppGrid> {
  final TextEditingController _search = TextEditingController();
  String _group = 'all';

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final language = Localizations.localeOf(context).languageCode;
    final query = _search.text.trim().toLowerCase();
    final groups = <(String, String)>[
      ('all', t.caGroupAll),
      ('coding', t.caGroupCoding),
      ('chat', t.caGroupChat),
      ('automation', t.caGroupAutomation),
      ('developer', t.caGroupDeveloper),
    ];
    final shown = widget.apps.where((app) {
      if (_group != 'all' && app['group'] != _group) return false;
      if (query.isEmpty) return true;
      return '${app['name']} ${localizedText(app['summary'], language)}'.toLowerCase().contains(query);
    }).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        TextField(
          controller: _search,
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(prefixIcon: const Icon(Icons.search_rounded, size: 18), hintText: t.caSearch),
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            for (final (id, label) in groups)
              ChoiceChip(label: Text(label), selected: _group == id, showCheckmark: false, onSelected: (_) => setState(() => _group = id)),
          ],
        ),
        const SizedBox(height: 16),
        if (shown.isEmpty)
          InlineHint(message: t.caNoApps)
        else
          LayoutBuilder(
            builder: (context, box) {
              final columns = box.maxWidth >= 720 ? 3 : box.maxWidth >= 440 ? 2 : 1;
              final width = (box.maxWidth - (columns - 1) * 10) / columns;
              return Wrap(
                spacing: 10,
                runSpacing: 10,
                children: <Widget>[for (final app in shown) SizedBox(width: width, child: _AppTile(app: app, onTap: () => widget.onPick(app)))],
              );
            },
          ),
      ],
    );
  }
}

class _AppTile extends StatefulWidget {
  const _AppTile({required this.app, required this.onTap});

  final Map<String, dynamic> app;
  final VoidCallback onTap;

  @override
  State<_AppTile> createState() => _AppTileState();
}

class _AppTileState extends State<_AppTile> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final language = Localizations.localeOf(context).languageCode;
    return MouseRegion(
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 150),
        decoration: BoxDecoration(
          color: _hover ? palette.surfaceRaised : palette.surface,
          borderRadius: BorderRadius.circular(AppRadius.control),
          border: Border.all(color: _hover ? palette.borderStrong : palette.border),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(AppRadius.control),
          onTap: widget.onTap,
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Row(
              children: <Widget>[
                ProviderAvatar(label: _appName(widget.app), size: 36),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(_appName(widget.app), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 14)),
                      const SizedBox(height: 2),
                      Text(localizedText(widget.app['summary'], language), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: palette.textSoft)),
                    ],
                  ),
                ),
                Icon(Icons.chevron_right_rounded, size: 18, color: _hover ? palette.text : palette.textMuted),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------- step 2

class _PlaceStep extends StatefulWidget {
  const _PlaceStep({
    super.key,
    required this.controller,
    required this.app,
    required this.places,
    required this.selected,
    required this.onSelect,
    required this.onPlaces,
    required this.onBack,
    required this.onContinue,
  });

  final NeoRouterController controller;
  final Map<String, dynamic> app;
  final List<Map<String, dynamic>> places;
  final String selected;
  final ValueChanged<String> onSelect;
  final ValueChanged<List<dynamic>> onPlaces;
  final VoidCallback onBack;
  final VoidCallback onContinue;

  @override
  State<_PlaceStep> createState() => _PlaceStepState();
}

class _PlaceStepState extends State<_PlaceStep> {
  // null · 'working' · 'restarting' · 'back' · 'manual'
  String? _opening;

  Map<String, dynamic> get _place => widget.places.firstWhere((place) => place['id'] == widget.selected, orElse: () => widget.places.first);

  Future<void> _open() async {
    setState(() => _opening = 'working');
    try {
      final result = await widget.controller.api.setNetworkScope('network');
      if (!mounted) return;
      if (result['restarting'] == true) {
        setState(() => _opening = 'restarting');
        await _waitForRestart();
      } else {
        setState(() => _opening = result['restartNeeded'] == true ? 'manual' : 'back');
        widget.onPlaces((result['places'] as List?) ?? const <dynamic>[]);
      }
    } on ApiException catch (error) {
      if (mounted) {
        setState(() => _opening = null);
        showSnack(context, error.message);
      }
    }
  }

  // NeoRouter stops and its service manager starts it again: wait until it answers.
  Future<void> _waitForRestart() async {
    await Future<void>.delayed(const Duration(seconds: 2));
    for (var attempt = 0; attempt < 40 && mounted; attempt++) {
      try {
        final data = await widget.controller.api.connectApps();
        if (!mounted) return;
        if ((data['network'] as Map?)?['scope'] == 'network') {
          widget.onPlaces((data['places'] as List?) ?? const <dynamic>[]);
          setState(() => _opening = 'back');
          return;
        }
      } catch (_) {
        // Still restarting.
      }
      await Future<void>.delayed(const Duration(milliseconds: 1500));
    }
    if (mounted) setState(() => _opening = 'manual');
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final name = _appName(widget.app);
    final cloudOnly = widget.app['cloudOnly'] == true;
    final place = _place;
    final reachable = place['reachable'] == true;
    final isAdmin = widget.controller.user?.isAdmin == true && !widget.controller.inSharedSpace;
    final options = <(String, IconData, String, String)>[
      ('local', Icons.laptop_mac_rounded, t.caPlaceLocal, t.caPlaceLocalHint),
      ('docker', Icons.inventory_2_outlined, t.caPlaceDocker, t.caPlaceDockerHint),
      ('network', Icons.devices_other_rounded, t.caPlaceNetwork, t.caPlaceNetworkHint),
      ('internet', Icons.public_rounded, t.caPlaceInternet, t.caPlaceInternetHint),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            ProviderAvatar(label: name, size: 32),
            const SizedBox(width: 12),
            Expanded(child: Text(t.caWhereTitle(name), style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600, color: palette.text))),
          ],
        ),
        const SizedBox(height: 14),
        if (cloudOnly) ...<Widget>[InlineHint(message: t.caCloudOnly(name)), const SizedBox(height: 12)],
        for (final (id, icon, title, hint) in options)
          _PlaceCard(
            icon: icon,
            title: title,
            hint: hint,
            place: widget.places.firstWhere((item) => item['id'] == id, orElse: () => <String, dynamic>{'id': id}),
            usual: '${widget.app['runs']}' == id && !cloudOnly,
            selected: widget.selected == id,
            enabled: !cloudOnly || id == 'internet',
            onTap: () => widget.onSelect(id),
          ),
        // What stands in the way, and the one thing that fixes it.
        if (!reachable && place['fix'] == 'open_network') ...<Widget>[
          const SizedBox(height: 6),
          _Notice(
            tone: PillTone.warning,
            title: _opening == 'back' ? t.caRestartBack : t.caOpenTitle,
            children: <Widget>[
              if (_opening == null || _opening == 'working') ...<Widget>[
                Text(isAdmin ? t.caOpenHint : t.caOpenAskAdmin, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
                if (isAdmin) ...<Widget>[
                  const SizedBox(height: 12),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: FilledButton.icon(
                      onPressed: _opening == null ? _open : null,
                      icon: _opening == 'working'
                          ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                          : const Icon(Icons.lock_open_rounded, size: 16),
                      label: Text(t.caOpenButton),
                    ),
                  ),
                ],
              ],
              if (_opening == 'restarting')
                Row(
                  children: <Widget>[
                    const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)),
                    const SizedBox(width: 10),
                    Expanded(child: Text(t.caRestarting, style: TextStyle(color: palette.textSoft, fontSize: 13))),
                  ],
                ),
              if (_opening == 'manual') ...<Widget>[
                Text(t.caRestartManual, style: TextStyle(color: palette.textSoft, fontSize: 13)),
                const SizedBox(height: 8),
                _CodeBlock(code: 'neorouter restart'),
              ],
            ],
          ),
        ],
        if (!reachable && place['fix'] == 'public_url') ...<Widget>[
          const SizedBox(height: 6),
          _Notice(tone: PillTone.warning, title: t.caNotReachable, children: <Widget>[Text(t.caPublicUrlHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45))]),
        ],
        if (widget.selected == 'network' && place['address'] == null) ...<Widget>[const SizedBox(height: 6), InlineHint(message: t.caNoLanAddress)],
        if (place['note'] == 'docker_host') ...<Widget>[
          const SizedBox(height: 6),
          _Notice(
            tone: PillTone.info,
            title: null,
            children: <Widget>[
              Text(t.caDockerLinuxHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
              const SizedBox(height: 8),
              const _CodeBlock(code: 'extra_hosts:\n  - "host.docker.internal:host-gateway"'),
            ],
          ),
        ],
        const SizedBox(height: 18),
        Row(
          children: <Widget>[
            TextButton.icon(onPressed: widget.onBack, icon: const Icon(Icons.arrow_back_rounded, size: 16), label: Text(t.caBack)),
            const Spacer(),
            FilledButton(onPressed: place['address'] == null ? null : widget.onContinue, child: Text(t.caContinue)),
          ],
        ),
      ],
    );
  }
}

class _PlaceCard extends StatelessWidget {
  const _PlaceCard({
    required this.icon,
    required this.title,
    required this.hint,
    required this.place,
    required this.usual,
    required this.selected,
    required this.enabled,
    required this.onTap,
  });

  final IconData icon;
  final String title;
  final String hint;
  final Map<String, dynamic> place;
  final bool usual;
  final bool selected;
  final bool enabled;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final reachable = place['reachable'] == true;
    final address = place['address'] as String?;
    return Opacity(
      opacity: enabled ? 1 : 0.45,
      child: Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: Material(
          color: selected ? palette.surfaceRaised : Colors.transparent,
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(AppRadius.control),
            side: BorderSide(color: selected ? palette.accent.withValues(alpha: 0.7) : palette.borderStrong),
          ),
          child: InkWell(
            borderRadius: BorderRadius.circular(AppRadius.control),
            onTap: enabled ? onTap : null,
            child: Padding(
              padding: const EdgeInsets.all(14),
              child: Row(
                children: <Widget>[
                  Icon(icon, size: 20, color: selected ? palette.accent : palette.textSoft),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Wrap(
                          spacing: 8,
                          runSpacing: 4,
                          crossAxisAlignment: WrapCrossAlignment.center,
                          children: <Widget>[
                            Text(title, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 14)),
                            if (usual) MetaPill(label: t.caUsual, tone: PillTone.accent),
                          ],
                        ),
                        const SizedBox(height: 2),
                        Text(hint, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.4)),
                        if (address != null) ...<Widget>[
                          const SizedBox(height: 6),
                          Text(address, style: GoogleFonts.geistMono(fontSize: 12, color: palette.textMuted)),
                        ],
                      ],
                    ),
                  ),
                  const SizedBox(width: 10),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      ToneDot(tone: reachable ? PillTone.success : PillTone.warning),
                      const SizedBox(width: 6),
                      Text(reachable ? t.caReachable : t.caNotReachable, style: TextStyle(fontSize: 12, color: palette.textSoft)),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------- step 3

class _SetupStep extends StatefulWidget {
  const _SetupStep({super.key, required this.controller, required this.app, required this.place, required this.onBack, required this.onAnother});

  final NeoRouterController controller;
  final Map<String, dynamic> app;
  final Map<String, dynamic> place;
  final VoidCallback onBack;
  final VoidCallback onAnother;

  @override
  State<_SetupStep> createState() => _SetupStepState();
}

class _SetupStepState extends State<_SetupStep> {
  String? _keyId;
  String? _secret;
  bool _existingKey = false;
  bool _creating = false;
  String _model = 'auto';
  Map<String, dynamic>? _activity;
  Timer? _poll;
  DateTime? _waitingSince;

  String get _name => _appName(widget.app);

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _createKey() async {
    setState(() => _creating = true);
    try {
      final created = await widget.controller.api.createKey(<String, Object?>{'name': _name});
      if (!mounted) return;
      setState(() {
        _keyId = created.key.id;
        _secret = created.secret;
        _waitingSince = DateTime.now();
      });
      _poll = Timer.periodic(const Duration(seconds: 2), (_) => _check());
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  Future<void> _check() async {
    final id = _keyId;
    if (id == null) return;
    try {
      final activity = await widget.controller.api.keyActivity(id);
      if (!mounted) return;
      setState(() => _activity = activity);
      // A working answer: nothing left to watch.
      if ((activity['request'] as Map?)?['ok'] == true) _poll?.cancel();
    } on ApiException {
      // Tried again on the next tick.
    }
  }

  Future<void> _pickModel() async {
    final t = AppL10n.of(context);
    List<RoutingProfile> profiles;
    try {
      profiles = await widget.controller.api.profiles();
    } on ApiException {
      profiles = <RoutingProfile>[];
    }
    if (!mounted) return;
    final picked = await showModelPicker(
      context,
      controller: widget.controller,
      selected: <String>[_model],
      routes: <(String, String)>[
        ('auto', t.caModelAutoRoute),
        for (final profile in profiles) ('auto:${profile.slug}', profile.name),
      ],
    );
    if (picked != null && picked.isNotEmpty && mounted) setState(() => _model = picked.first);
  }

  Map<String, String> get _values {
    final baseUrl = '${widget.place['address']}';
    return <String, String>{
      'baseUrl': baseUrl,
      'rootUrl': baseUrl.replaceFirst(RegExp(r'/v1$'), ''),
      'apiKey': _secret ?? 'nrt_…',
      'model': _model,
    };
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final language = Localizations.localeOf(context).languageCode;
    final values = _values;
    final steps = ((widget.app['steps'] as List?) ?? const <Object>[]).map((step) => _fill(localizedText(step, language), values)).toList();
    final fields = ((widget.app['fields'] as List?) ?? const <Object>[]).map((field) => Map<String, dynamic>.from(field as Map)).toList();
    final file = widget.app['file'] is Map ? Map<String, dynamic>.from(widget.app['file'] as Map) : null;
    final hasKey = _secret != null || _existingKey;
    final mayCreate = widget.controller.can('keys.create');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        // 1. A key of its own.
        _Section(
          number: 1,
          title: t.caKeyTitle(_name),
          done: hasKey,
          child: _secret != null
              ? _Notice(tone: PillTone.success, title: null, children: <Widget>[Text(t.caKeyCreated, style: TextStyle(color: palette.textSoft, fontSize: 13))])
              : _existingKey
              ? Text(t.caKeyExistingHint, style: TextStyle(color: palette.textSoft, fontSize: 13))
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(t.caKeyCreateHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
                    const SizedBox(height: 12),
                    Wrap(
                      spacing: 10,
                      runSpacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: <Widget>[
                        if (mayCreate)
                          FilledButton.icon(
                            onPressed: _creating ? null : _createKey,
                            icon: _creating ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.key_rounded, size: 16),
                            label: Text(t.caKeyCreate(_name)),
                          ),
                        TextButton(onPressed: () => setState(() => _existingKey = true), child: Text(t.caKeyExisting)),
                      ],
                    ),
                  ],
                ),
        ),
        // 2. The model, auto unless picked.
        _Section(
          number: 2,
          title: t.caModelTitle,
          done: true,
          child: Row(
            children: <Widget>[
              Expanded(
                child: _model == 'auto'
                    ? Text(t.caModelAuto, style: TextStyle(color: palette.textSoft, fontSize: 13))
                    : Text(_model, style: GoogleFonts.geistMono(fontSize: 13, color: palette.text)),
              ),
              TextButton(onPressed: _pickModel, child: Text(t.caModelChange)),
            ],
          ),
        ),
        // 3. What to do in the app, with the values filled in.
        _Section(
          number: 3,
          title: t.caInApp(_name),
          done: false,
          dimmed: !hasKey,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              for (var i = 0; i < steps.length; i++)
                Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text('${i + 1}.', style: TextStyle(color: palette.textMuted, fontSize: 13, height: 1.45)),
                      const SizedBox(width: 8),
                      Expanded(child: Text(steps[i], style: TextStyle(color: palette.text, fontSize: 13.5, height: 1.45))),
                    ],
                  ),
                ),
              if (fields.isNotEmpty) ...<Widget>[
                const SizedBox(height: 6),
                Container(
                  decoration: BoxDecoration(border: Border.all(color: palette.border), borderRadius: BorderRadius.circular(AppRadius.control)),
                  child: Column(
                    children: <Widget>[
                      for (var i = 0; i < fields.length; i++) ...<Widget>[
                        if (i > 0) Divider(height: 1, color: palette.border),
                        _CopyField(label: '${fields[i]['label']}', value: values['${fields[i]['value']}'] ?? ''),
                      ],
                    ],
                  ),
                ),
              ],
              if (file != null) ...<Widget>[
                const SizedBox(height: 10),
                _FileBlock(name: '${file['name']}', code: _fill('${file['template']}', values)),
              ],
            ],
          ),
        ),
        // 4. Did it work?
        if (_keyId != null) ...<Widget>[
          const SizedBox(height: 4),
          _LiveCheck(app: _name, activity: _activity, since: _waitingSince, placeId: '${widget.place['id']}'),
        ],
        const SizedBox(height: 18),
        Row(
          children: <Widget>[
            TextButton.icon(onPressed: widget.onBack, icon: const Icon(Icons.arrow_back_rounded, size: 16), label: Text(t.caBack)),
            const Spacer(),
            OutlinedButton(onPressed: widget.onAnother, child: Text(t.caAnother)),
          ],
        ),
      ],
    );
  }
}

/// A numbered part of the setup.
class _Section extends StatelessWidget {
  const _Section({required this.number, required this.title, required this.done, required this.child, this.dimmed = false});

  final int number;
  final String title;
  final bool done;
  final bool dimmed;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return AnimatedOpacity(
      duration: const Duration(milliseconds: 200),
      opacity: dimmed ? 0.5 : 1,
      child: Container(
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.all(18),
        decoration: BoxDecoration(color: palette.surface, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: palette.border)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(
              children: <Widget>[
                Container(
                  width: 22,
                  height: 22,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(shape: BoxShape.circle, color: done ? palette.success.withValues(alpha: 0.18) : palette.surfaceRaised),
                  child: done
                      ? Icon(Icons.check_rounded, size: 13, color: palette.success)
                      : Text('$number', style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w700, color: palette.textSoft)),
                ),
                const SizedBox(width: 10),
                Expanded(child: Text(title, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text))),
              ],
            ),
            const SizedBox(height: 12),
            child,
          ],
        ),
      ),
    );
  }
}

/// The app's field name on the left, the value to paste on the right.
class _CopyField extends StatelessWidget {
  const _CopyField({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(14, 8, 4, 8),
      child: LayoutBuilder(
        builder: (context, box) {
          final narrow = box.maxWidth < 460;
          final labelText = Text(label, style: TextStyle(fontSize: 12.5, color: palette.textSoft));
          final valueText = SelectableText(value, style: GoogleFonts.geistMono(fontSize: 12.5, color: palette.text));
          final copy = IconButton(
            tooltip: t.copy,
            icon: const Icon(Icons.copy_rounded, size: 16),
            onPressed: () {
              Clipboard.setData(ClipboardData(text: value));
              showSnack(context, t.copied);
            },
          );
          if (narrow) {
            return Row(
              children: <Widget>[
                Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: <Widget>[labelText, const SizedBox(height: 2), valueText])),
                copy,
              ],
            );
          }
          return Row(
            children: <Widget>[
              SizedBox(width: 190, child: labelText),
              Expanded(child: valueText),
              copy,
            ],
          );
        },
      ),
    );
  }
}

class _FileBlock extends StatelessWidget {
  const _FileBlock({required this.name, required this.code});

  final String name;
  final String code;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final downloadable = name.contains('.');
    return Container(
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: palette.border)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 4, 4, 0),
            child: Row(
              children: <Widget>[
                Icon(downloadable ? Icons.description_outlined : Icons.terminal_rounded, size: 15, color: palette.textMuted),
                const SizedBox(width: 8),
                Expanded(child: Text(name, style: GoogleFonts.geistMono(fontSize: 12, color: palette.textSoft))),
                if (downloadable)
                  IconButton(
                    tooltip: t.caDownload,
                    icon: const Icon(Icons.download_rounded, size: 16),
                    onPressed: () => downloadBytes(Uint8List.fromList(utf8.encode(code)), 'text/plain', name),
                  ),
                IconButton(
                  tooltip: t.copy,
                  icon: const Icon(Icons.copy_rounded, size: 16),
                  onPressed: () {
                    Clipboard.setData(ClipboardData(text: code));
                    showSnack(context, t.copied);
                  },
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 0, 14, 14),
            child: SelectableText(code, style: GoogleFonts.geistMono(fontSize: 12.5, height: 1.55, color: palette.text)),
          ),
        ],
      ),
    );
  }
}

class _CodeBlock extends StatelessWidget {
  const _CodeBlock({required this.code});

  final String code;

  @override
  Widget build(BuildContext context) => _FileBlock(name: 'Terminal', code: code);
}

/// A tinted box with an optional title.
class _Notice extends StatelessWidget {
  const _Notice({required this.tone, required this.title, required this.children});

  final PillTone tone;
  final String? title;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final color = toneColor(palette, tone);
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(AppRadius.control),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (title != null) ...<Widget>[
            Text(title!, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 13.5)),
            const SizedBox(height: 6),
          ],
          ...children,
        ],
      ),
    );
  }
}

/// "Waiting for Pi Dev …", then connected, or what went wrong and how to fix it.
class _LiveCheck extends StatefulWidget {
  const _LiveCheck({required this.app, required this.activity, required this.since, required this.placeId});

  final String app;
  final Map<String, dynamic>? activity;
  final DateTime? since;
  final String placeId;

  @override
  State<_LiveCheck> createState() => _LiveCheckState();
}

class _LiveCheckState extends State<_LiveCheck> with SingleTickerProviderStateMixin {
  late final AnimationController _pulse = AnimationController(vsync: this, duration: const Duration(milliseconds: 1400))..repeat(reverse: true);

  @override
  void dispose() {
    _pulse.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final activity = widget.activity;
    final request = activity?['request'] is Map ? Map<String, dynamic>.from(activity!['request'] as Map) : null;
    final seen = activity?['lastUsedAt'] != null;
    final text = TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45);
    final waitedLong = widget.since != null && DateTime.now().difference(widget.since!) > const Duration(seconds: 60);

    if (request?['ok'] == true) {
      final latency = request!['latencyMs'] as num?;
      final seconds = latency == null ? null : (latency / 1000).toStringAsFixed(1);
      final time = seconds == null ? '' : ' · ${Localizations.localeOf(context).languageCode == 'de' ? seconds.replaceAll('.', ',') : seconds} s';
      return _Notice(
        tone: PillTone.success,
        title: null,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(Icons.check_circle_rounded, color: palette.success, size: 22),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(t.caConnected(widget.app), style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 14)),
                    const SizedBox(height: 2),
                    Text(t.caConnectedDetail('${request['model'] ?? ''}', time), style: text),
                  ],
                ),
              ),
            ],
          ),
        ],
      );
    }
    if (request != null) {
      final unknownModel = request['errorCode'] == 'MODEL_NOT_FOUND';
      final message = '${request['errorMessage'] ?? ''}';
      final suggestions = RegExp(r'"([^"]+)"').allMatches(message.split('Did you mean').length > 1 ? message.split('Did you mean')[1] : '').map((match) => match.group(1)!).where((id) => id != 'auto').toList();
      return _Notice(
        tone: PillTone.warning,
        title: t.caFailed(widget.app),
        children: <Widget>[
          if (unknownModel) ...<Widget>[
            Text(t.caUnknownModel(widget.app, '${request['requested'] ?? ''}'), style: text),
            if (suggestions.isNotEmpty) ...<Widget>[
              const SizedBox(height: 8),
              Text(t.caDidYouMean(widget.app), style: text),
              const SizedBox(height: 8),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: <Widget>[
                  for (final id in <String>[...suggestions, 'auto'])
                    ActionChip(
                      avatar: const Icon(Icons.copy_rounded, size: 14),
                      label: Text(id, style: GoogleFonts.geistMono(fontSize: 12)),
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: id));
                        showSnack(context, t.copied);
                      },
                    ),
                ],
              ),
            ],
          ] else
            Text(message, style: text),
        ],
      );
    }
    if (seen) {
      return _Notice(tone: PillTone.success, title: t.caSeen(widget.app), children: <Widget>[Text(t.caSeenHint(widget.app), style: text)]);
    }
    return _Notice(
      tone: PillTone.neutral,
      title: null,
      children: <Widget>[
        Row(
          children: <Widget>[
            FadeTransition(
              opacity: Tween<double>(begin: 0.25, end: 1).animate(_pulse),
              child: Container(width: 10, height: 10, decoration: BoxDecoration(color: palette.accent, shape: BoxShape.circle)),
            ),
            const SizedBox(width: 12),
            Expanded(child: Text(t.caWaiting(widget.app), style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 13.5))),
          ],
        ),
        const SizedBox(height: 6),
        Text(t.caWaitingHint(widget.app), style: text),
        if (waitedLong) ...<Widget>[
          const SizedBox(height: 10),
          Text(t.caNothingYet, style: text),
          for (final check in <String>[t.caCheckAddress, t.caCheckKey, if (widget.placeId == 'local') t.caCheckDocker(widget.app)])
            Padding(padding: const EdgeInsets.only(top: 4, left: 4), child: Text('• $check', style: text)),
        ],
      ],
    );
  }
}
