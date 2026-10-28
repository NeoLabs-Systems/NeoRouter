import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_models.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> presetOrder = <String>['balanced', 'fast', 'smart', 'reliable', 'best', 'cheap'];

String presetLabel(AppL10n t, String id) => switch (id) {
      'reliable' => t.presetReliable,
      'fast' => t.presetFast,
      'balanced' => t.presetBalanced,
      'best' => t.presetBest,
      'smart' => t.presetSmart,
      'cheap' => t.presetCheap,
      _ => t.presetCustom,
    };

String presetHint(AppL10n t, String id) => switch (id) {
      'reliable' => t.presetReliableHint,
      'fast' => t.presetFastHint,
      'balanced' => t.presetBalancedHint,
      'best' => t.presetBestHint,
      'smart' => t.presetSmartHint,
      'cheap' => t.presetCheapHint,
      _ => '',
    };

IconData presetIcon(String id) => switch (id) {
      'reliable' => Icons.verified_outlined,
      'fast' => Icons.bolt_outlined,
      'balanced' => Icons.tune_rounded,
      'best' => Icons.workspace_premium_outlined,
      'smart' => Icons.psychology_outlined,
      'cheap' => Icons.savings_outlined,
      _ => Icons.auto_fix_high_outlined,
    };

String profileLabel(AppL10n t, RoutingProfile profile) => profile.preset ? presetLabel(t, profile.id) : profile.name;

class RoutingScreen extends StatefulWidget {
  const RoutingScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<RoutingScreen> createState() => _RoutingScreenState();
}

class _RoutingScreenState extends State<RoutingScreen> {
  List<RoutingProfile> _profiles = <RoutingProfile>[];
  bool _loading = true;
  String? _error;
  String _selected = 'balanced';

  final TextEditingController _prompt = TextEditingController();
  bool _previewFree = true;
  Map<String, dynamic>? _preview;
  bool _previewing = false;
  bool _showSkipped = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _prompt.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final profiles = await widget.controller.api.profiles();
      setState(() {
        _profiles = profiles;
        if (!profiles.any((item) => item.id == _selected)) _selected = 'balanced';
      });
      await _runPreview();
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _select(String id) {
    setState(() => _selected = id);
    _runPreview();
  }

  Future<void> _runPreview() async {
    final t = AppL10n.of(context);
    setState(() => _previewing = true);
    try {
      final result = await widget.controller.api.preview(<String, Object?>{
        'profile': _selected,
        'mode': _previewFree ? 'free_only' : 'both',
        'prompt': _prompt.text.trim().isEmpty ? t.previewDefaultPrompt : _prompt.text,
      });
      setState(() => _preview = result);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _previewing = false);
    }
  }

  Future<void> _editStyle([RoutingProfile? profile]) async {
    final saved = await showDialog<String>(context: context, builder: (_) => _StyleEditor(controller: widget.controller, profile: profile));
    if (saved == null) return;
    if (saved.isNotEmpty) _selected = saved;
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final presets = <RoutingProfile>[
      for (final id in presetOrder) ..._profiles.where((item) => item.preset && item.id == id),
    ];
    final custom = _profiles.where((item) => !item.preset).toList();
    return PageScaffold(
      header: ScreenHeader(title: t.routingTitle, description: t.routingDescription),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              SettingsHeading(title: t.stylesTitle, description: t.stylesHint),
              LayoutBuilder(
                builder: (context, constraints) {
                  final columns = constraints.maxWidth >= 900 ? 3 : constraints.maxWidth >= 560 ? 2 : 1;
                  final width = (constraints.maxWidth - (columns - 1) * AppSpacing.sm) / columns;
                  return Wrap(
                    spacing: AppSpacing.sm,
                    runSpacing: AppSpacing.sm,
                    children: <Widget>[
                      for (final profile in presets)
                        SizedBox(
                          width: width,
                          child: _StyleCard(
                            icon: presetIcon(profile.id),
                            title: presetLabel(t, profile.id),
                            description: presetHint(t, profile.id),
                            modelName: profile.id == 'balanced' ? 'auto' : 'auto:${profile.id}',
                            selected: _selected == profile.id,
                            onTap: () => _select(profile.id),
                          ),
                        ),
                    ],
                  );
                },
              ),
              const SizedBox(height: 28),
              SettingsHeading(
                title: t.customStylesTitle,
                description: t.customStylesHint,
                trailing: OutlinedButton.icon(onPressed: () => _editStyle(), icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.createStyle)),
              ),
              if (custom.isNotEmpty)
                SettingsCardList(
                  children: <Widget>[
                    for (final profile in custom)
                      ListRow(
                        onTap: () => _select(profile.id),
                        leading: Icon(_selected == profile.id ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded, size: 18, color: _selected == profile.id ? palette.accent : palette.textMuted),
                        title: profile.name,
                        subtitle: 'auto:${profile.id}',
                        trailing: TextButton(onPressed: () => _editStyle(profile), child: Text(t.edit)),
                      ),
                  ],
                ),
              const SizedBox(height: 28),
              _previewSection(context),
            ],
          ),
        ),
      ],
    );
  }

  Widget _previewSection(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final candidates = ((_preview?['candidates'] as List?) ?? const <Object>[]).cast<Map>();
    final chosen = candidates.where((item) => item['rejected'] == null).take(5).toList();
    final skipped = candidates.where((item) => item['rejected'] != null).toList();
    final current = _profiles.where((item) => item.id == _selected).firstOrNull;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(
          title: t.previewTitle,
          description: t.previewHint(current == null ? '' : profileLabel(t, current)),
          trailing: _previewing ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : null,
        ),
        AppCard(
          padding: EdgeInsets.zero,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 18, 20, 14),
                child: Row(
                  children: <Widget>[
                    Expanded(
                      child: TextField(
                        controller: _prompt,
                        onSubmitted: (_) => _runPreview(),
                        decoration: InputDecoration(hintText: t.previewDefaultPrompt),
                      ),
                    ),
                    const SizedBox(width: AppSpacing.sm),
                    OutlinedButton(onPressed: _previewing ? null : _runPreview, child: Text(t.runPreview)),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 12, 8),
                child: Row(
                  children: <Widget>[
                    Expanded(child: Text(t.previewFreeOnly, style: TextStyle(color: palette.textSoft, fontSize: 13.5))),
                    Switch(
                      value: _previewFree,
                      onChanged: (value) {
                        setState(() => _previewFree = value);
                        _runPreview();
                      },
                    ),
                  ],
                ),
              ),
              Divider(height: 1, color: palette.border),
              if (chosen.isEmpty)
                Padding(padding: const EdgeInsets.all(20), child: Text(t.previewNone, style: TextStyle(color: palette.textSoft)))
              else
                for (var i = 0; i < chosen.length; i++) ...<Widget>[
                  if (i > 0) Divider(height: 1, color: palette.border),
                  _CandidateRow(rank: i + 1, candidate: chosen[i]),
                ],
              if (skipped.isNotEmpty) ...<Widget>[
                Divider(height: 1, color: palette.border),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: () => setState(() => _showSkipped = !_showSkipped),
                      icon: Icon(_showSkipped ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                      label: Text(t.previewSkipped('${skipped.length}')),
                    ),
                  ),
                ),
                if (_showSkipped)
                  for (final candidate in skipped.take(20))
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 2, 20, 8),
                      child: Row(
                        children: <Widget>[
                          Expanded(child: Text(shortModelName('${candidate['id']}'), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, color: palette.textMuted))),
                          Text(routingReasonLabel(t, candidate['rejected'] as String?), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                        ],
                      ),
                    ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _StyleCard extends StatelessWidget {
  const _StyleCard({required this.icon, required this.title, required this.description, required this.modelName, required this.selected, required this.onTap});

  final IconData icon;
  final String title;
  final String description;
  final String modelName;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final radius = BorderRadius.circular(AppRadius.card);
    return Material(
      color: selected ? palette.surfaceRaised : palette.surface,
      shape: RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: selected ? palette.text.withValues(alpha: 0.45) : palette.border)),
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Icon(icon, size: 20, color: selected ? palette.accent : palette.textSoft),
                  const Spacer(),
                  Text(modelName, style: GoogleFonts.geistMono(fontSize: 11.5, color: palette.textMuted)),
                ],
              ),
              const SizedBox(height: 14),
              Text(title, style: TextStyle(fontWeight: FontWeight.w600, fontSize: 14.5, color: palette.text)),
              const SizedBox(height: 4),
              Text(description, maxLines: 2, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.4)),
            ],
          ),
        ),
      ),
    );
  }
}

class _CandidateRow extends StatelessWidget {
  const _CandidateRow({required this.rank, required this.candidate});

  final int rank;
  final Map candidate;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final id = '${candidate['id']}';
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
      child: Row(
        children: <Widget>[
          SizedBox(width: 28, child: Text('$rank', style: TextStyle(color: rank == 1 ? palette.accent : palette.textMuted, fontWeight: FontWeight.w600))),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(shortModelName(id), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: palette.text)),
                const SizedBox(height: 2),
                Text(
                  '${id.split('/').first}${candidate['isFree'] == true ? ' · ${t.free}' : ''}${rank == 1 ? ' · ${t.previewMostLikely}' : ''}',
                  style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Create or edit an own routing style: what matters how much, plus limits.
class _StyleEditor extends StatefulWidget {
  const _StyleEditor({required this.controller, this.profile});

  final NeoRouterController controller;
  final RoutingProfile? profile;

  @override
  State<_StyleEditor> createState() => _StyleEditorState();
}

class _StyleEditorState extends State<_StyleEditor> {
  late final RoutingWeights _weights = widget.profile?.weights.copy() ?? RoutingWeights(intelligence: 0.3, speed: 0.2, reliability: 0.3, cost: 0.2);
  late final Map<String, dynamic> _constraints = widget.profile?.constraints ?? <String, dynamic>{};
  late final TextEditingController _name = TextEditingController(text: widget.profile?.name ?? '');
  late final TextEditingController _maxPrice = TextEditingController(text: _constraints['maxPriceOut']?.toString() ?? '');
  late final TextEditingController _exclude = TextEditingController(text: ((_constraints['excludeProviders'] as List?) ?? const <Object>[]).join(', '));
  late double _minIntelligence = (_constraints['minIntelligence'] as num?)?.toDouble() ?? 0;
  late double _exploration = (_constraints['exploration'] as num?)?.toDouble() ?? 0.08;
  late bool _adaptive = _constraints['adaptive'] != false;
  bool _advanced = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _maxPrice.dispose();
    _exclude.dispose();
    super.dispose();
  }

  Map<String, Object?> _constraintBody() => <String, Object?>{
        'minIntelligence': _minIntelligence > 0 ? _minIntelligence : null,
        'maxPriceOut': double.tryParse(_maxPrice.text.trim().replaceAll(',', '.')),
        'excludeProviders': _exclude.text.split(',').map((item) => item.trim()).where((item) => item.isNotEmpty).toList(),
        'adaptive': _adaptive,
        'exploration': _exploration,
      };

  Future<void> _save() async {
    final t = AppL10n.of(context);
    final name = _name.text.trim().isEmpty ? t.presetCustom : _name.text.trim();
    try {
      final api = widget.controller.api;
      final profile = widget.profile == null
          ? await api.createProfile(name, _weights, _constraintBody())
          : await api.updateProfileWeights(widget.profile!.id, name: name, weights: _weights, constraints: _constraintBody());
      if (mounted) Navigator.of(context).pop(profile.id);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    }
  }

  Future<void> _delete() async {
    await widget.controller.api.deleteProfile(widget.profile!.id);
    if (mounted) Navigator.of(context).pop('');
  }

  Widget _slider(String label, double value, ValueChanged<double> onChanged, {double min = 0, double max = 1}) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      children: <Widget>[
        SizedBox(width: 110, child: Text(label, style: TextStyle(fontSize: 13.5, color: palette.text))),
        Expanded(child: Slider(value: value.clamp(min, max), min: min, max: max, onChanged: (next) => setState(() => onChanged(next)))),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return AlertDialog(
      title: Text(widget.profile == null ? t.createStyle : t.editStyle),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _name, autofocus: widget.profile == null, decoration: InputDecoration(labelText: t.profileName)),
              const SizedBox(height: AppSpacing.lg),
              Text(t.weightsTitle, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
              const SizedBox(height: 2),
              Text(t.weightsHint, style: TextStyle(fontSize: 12.5, color: palette.textSoft)),
              const SizedBox(height: AppSpacing.xs),
              _slider(t.intelligence, _weights.intelligence, (value) => _weights.intelligence = value),
              _slider(t.speed, _weights.speed, (value) => _weights.speed = value),
              _slider(t.reliability, _weights.reliability, (value) => _weights.reliability = value),
              _slider(t.cost, _weights.cost, (value) => _weights.cost = value),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _advanced = !_advanced),
                  icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.advancedOptions),
                ),
              ),
              if (_advanced) ...<Widget>[
                _slider(t.minIntelligence, _minIntelligence, (value) => _minIntelligence = value, max: 0.9),
                _slider(t.exploration, _exploration, (value) => _exploration = value, min: 0.01, max: 0.3),
                Text(t.explorationHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  value: _adaptive,
                  onChanged: (value) => setState(() => _adaptive = value),
                  title: Text(t.adaptive, style: const TextStyle(fontSize: 13.5)),
                  subtitle: Text(t.adaptiveHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                ),
                TextField(controller: _maxPrice, keyboardType: const TextInputType.numberWithOptions(decimal: true), decoration: InputDecoration(labelText: t.maxPriceOut)),
                const SizedBox(height: AppSpacing.sm),
                TextField(controller: _exclude, decoration: InputDecoration(labelText: t.excludeProviders)),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        if (widget.profile != null) TextButton(onPressed: _delete, style: TextButton.styleFrom(foregroundColor: palette.error), child: Text(t.delete)),
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _save, child: Text(t.save)),
      ],
    );
  }
}
