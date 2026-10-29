import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_models.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> presetOrder = <String>['balanced', 'fast', 'smart'];

String presetLabel(AppL10n t, String id) => switch (id) {
      'fast' => t.presetFast,
      'balanced' => t.presetBalanced,
      'smart' => t.presetSmart,
      _ => t.presetCustom,
    };

String presetHint(AppL10n t, String id) => switch (id) {
      'fast' => t.presetFastHint,
      'balanced' => t.presetBalancedHint,
      'smart' => t.presetSmartHint,
      _ => '',
    };

IconData presetIcon(String id) => switch (id) {
      'fast' => Icons.bolt_outlined,
      'balanced' => Icons.tune_rounded,
      'smart' => Icons.psychology_outlined,
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

  bool _previewFree = true;
  Map<String, dynamic>? _preview;
  bool _previewing = false;
  bool _showSkipped = false;

  @override
  void initState() {
    super.initState();
    _load();
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
    setState(() {
      _selected = id;
      _showSkipped = false;
    });
    _runPreview();
  }

  Future<void> _runPreview() async {
    final t = AppL10n.of(context);
    setState(() => _previewing = true);
    try {
      final result = await widget.controller.api.preview(<String, Object?>{
        'profile': _selected,
        'mode': _previewFree ? 'free_only' : 'both',
        'prompt': t.previewDefaultPrompt,
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
    _selected = saved.isNotEmpty ? saved : 'balanced';
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final presets = <RoutingProfile>[
      for (final id in presetOrder) ..._profiles.where((item) => item.preset && item.id == id),
    ];
    final custom = _profiles.where((item) => !item.preset).toList();
    final current = _profiles.where((item) => item.id == _selected).firstOrNull;
    return PageScaffold(
      header: ScreenHeader(title: t.routingTitle, description: t.routingDescription),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: LayoutBuilder(
            builder: (context, constraints) {
              final rail = _StyleRail(
                presets: presets,
                custom: custom,
                selected: _selected,
                onSelect: _select,
                onCreate: widget.controller.can('routing.edit') ? () => _editStyle() : null,
                compact: constraints.maxWidth < 820,
              );
              final detail = current == null ? const SizedBox.shrink() : _detail(context, current);
              if (constraints.maxWidth < 820) {
                return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: <Widget>[rail, const SizedBox(height: AppSpacing.lg), detail]);
              }
              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  SizedBox(width: 248, child: rail),
                  const SizedBox(width: 32),
                  Expanded(child: detail),
                ],
              );
            },
          ),
        ),
      ],
    );
  }

  Widget _detail(BuildContext context, RoutingProfile profile) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final modelName = 'auto:${profile.id}';
    final weights = profile.weights;
    final total = (weights.intelligence + weights.speed + weights.reliability + weights.cost).clamp(0.0001, double.infinity);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text((profile.preset ? t.styleBuiltIn : t.customStylesTitle).toUpperCase(), style: sectionEyebrowStyle(palette)),
        const SizedBox(height: 8),
        Row(
          children: <Widget>[
            Icon(presetIcon(profile.preset ? profile.id : ''), size: 24, color: palette.accent),
            const SizedBox(width: 12),
            Expanded(child: Text(profileLabel(t, profile), style: displayTitleStyle(palette, size: 26))),
            if (!profile.preset && widget.controller.can('routing.edit'))
              OutlinedButton.icon(onPressed: () => _editStyle(profile), icon: const Icon(Icons.edit_outlined, size: 16), label: Text(t.edit)),
          ],
        ),
        if (!profile.preset && widget.controller.showCreators) ...<Widget>[
          const SizedBox(height: 6),
          Text(createdByLabel(t, profile.createdBy), style: TextStyle(color: palette.textMuted, fontSize: 13)),
        ],
        if (profile.preset) ...<Widget>[
          const SizedBox(height: 8),
          Text(presetHint(t, profile.id), style: TextStyle(color: palette.textSoft, fontSize: 14.5, height: 1.45)),
        ],
        const SizedBox(height: AppSpacing.sm),
        InlineHint(message: t.presetCheaperNote, icon: Icons.savings_outlined),
        const SizedBox(height: AppSpacing.lg),
        SettingsGroup(
          title: t.styleUseTitle,
          description: t.styleUseHint,
          children: <Widget>[
            SettingsRow(
              label: modelName,
              description: t.styleUseRow,
              control: OutlinedButton.icon(
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: modelName));
                  showSnack(context, t.copied);
                },
                icon: const Icon(Icons.copy_rounded, size: 15),
                label: Text(t.copy),
              ),
            ),
          ],
        ),
        SettingsGroup(
          title: t.weightsTitle,
          description: t.styleWeightsHint,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
              child: Column(
                children: <Widget>[
                  for (final (label, value) in <(String, double)>[
                    (t.intelligence, weights.intelligence),
                    (t.speed, weights.speed),
                    (t.reliability, weights.reliability),
                    (t.cost, weights.cost),
                  ])
                    _WeightBar(label: label, share: value / total),
                ],
              ),
            ),
          ],
        ),
        _previewGroup(context, profile),
      ],
    );
  }

  Widget _previewGroup(BuildContext context, RoutingProfile profile) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final candidates = ((_preview?['candidates'] as List?) ?? const <Object>[]).cast<Map>();
    final chosen = candidates.where((item) => item['rejected'] == null).take(3).toList();
    final skipped = candidates.where((item) => item['rejected'] != null).toList();
    return SettingsGroup(
      title: t.previewTitle,
      description: t.previewHint(profileLabel(t, profile)),
      trailing: _previewing ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : null,
      children: <Widget>[
        SettingsRow(
          label: t.previewFreeOnly,
          control: Switch(
            value: _previewFree,
            onChanged: (value) {
              setState(() => _previewFree = value);
              _runPreview();
            },
          ),
        ),
        if (chosen.isEmpty)
          Padding(padding: const EdgeInsets.all(20), child: Text(t.previewNone, style: TextStyle(color: palette.textSoft)))
        else
          for (var i = 0; i < chosen.length; i++) _CandidateRow(rank: i + 1, candidate: chosen[i]),
        if (skipped.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: () => setState(() => _showSkipped = !_showSkipped),
                    icon: Icon(_showSkipped ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                    label: Text(t.previewSkipped('${skipped.length}')),
                  ),
                ),
                if (_showSkipped)
                  for (final candidate in skipped.take(20))
                    Padding(
                      padding: const EdgeInsets.fromLTRB(12, 2, 12, 8),
                      child: Row(
                        children: <Widget>[
                          Expanded(child: Text(shortModelName('${candidate['id']}'), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, color: palette.textMuted))),
                          Text(routingReasonLabel(t, candidate['rejected'] as String?), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                        ],
                      ),
                    ),
              ],
            ),
          ),
      ],
    );
  }
}

/// The styles as a NeoLabs settings rail: built-in first, then your own.
class _StyleRail extends StatelessWidget {
  const _StyleRail({required this.presets, required this.custom, required this.selected, required this.onSelect, required this.onCreate, required this.compact});

  final List<RoutingProfile> presets;
  final List<RoutingProfile> custom;
  final String selected;
  final ValueChanged<String> onSelect;
  final VoidCallback? onCreate;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final all = <RoutingProfile>[...presets, ...custom];
    if (compact) {
      return SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: <Widget>[
            for (final profile in all)
              Padding(
                padding: const EdgeInsets.only(right: 6),
                child: ChoiceChip(
                  avatar: Icon(presetIcon(profile.preset ? profile.id : ''), size: 16),
                  label: Text(profileLabel(t, profile)),
                  selected: selected == profile.id,
                  onSelected: (_) => onSelect(profile.id),
                ),
              ),
            if (onCreate != null) ActionChip(avatar: const Icon(Icons.add_rounded, size: 16), label: Text(t.createStyle), onPressed: onCreate),
          ],
        ),
      );
    }
    Widget item(RoutingProfile profile) {
      final active = selected == profile.id;
      return Padding(
        padding: const EdgeInsets.only(bottom: 2),
        child: Material(
          color: active ? palette.accentSoft : Colors.transparent,
          borderRadius: BorderRadius.circular(10),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: () => onSelect(profile.id),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
              child: Row(
                children: <Widget>[
                  Icon(presetIcon(profile.preset ? profile.id : ''), size: 18, color: active ? palette.accent : palette.textMuted),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      profileLabel(t, profile),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 14, fontWeight: active ? FontWeight.w600 : FontWeight.w500, color: active ? palette.text : palette.textSoft),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(padding: const EdgeInsets.fromLTRB(10, 0, 10, 8), child: Text(t.styleBuiltIn.toUpperCase(), style: sectionEyebrowStyle(palette))),
        for (final profile in presets) item(profile),
        const SizedBox(height: 18),
        Padding(padding: const EdgeInsets.fromLTRB(10, 0, 10, 8), child: Text(t.customStylesTitle.toUpperCase(), style: sectionEyebrowStyle(palette))),
        for (final profile in custom) item(profile),
        if (onCreate != null)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(onPressed: onCreate, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.createStyle)),
          ),
      ],
    );
  }
}

/// One weight as a share of the whole, e.g. "Smart ▇▇▇▇▁▁ 30 %".
class _WeightBar extends StatelessWidget {
  const _WeightBar({required this.label, required this.share});

  final String label;
  final double share;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: <Widget>[
          SizedBox(width: 110, child: Text(label, style: TextStyle(fontSize: 13.5, color: palette.text))),
          Expanded(
            child: ClipRRect(
              borderRadius: BorderRadius.circular(AppRadius.pill),
              child: LinearProgressIndicator(
                minHeight: 6,
                value: share.clamp(0.0, 1.0),
                backgroundColor: palette.surfaceRaised,
                valueColor: AlwaysStoppedAnimation<Color>(palette.accent),
              ),
            ),
          ),
          SizedBox(width: 52, child: Text('${(share * 100).round()} %', textAlign: TextAlign.end, style: TextStyle(fontSize: 12.5, color: palette.textMuted))),
        ],
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
  // How much free, subscription and paid models count (0 = not used).
  late final Map<String, double> _kinds = <String, double>{
    for (final kind in const <String>['free', 'plan', 'paid']) kind: ((_constraints['kinds'] as Map?)?[kind] as num?)?.toDouble() ?? 1,
  };
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
        // Every kind at full weight is the same as no preference.
        'kinds': _kinds.values.every((value) => value >= 1)
            ? null
            : <String, double>{for (final entry in _kinds.entries) entry.key: entry.value < 0.01 ? 0 : entry.value},
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
              const SizedBox(height: AppSpacing.md),
              Text(t.styleKindsTitle, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
              const SizedBox(height: 2),
              Text(t.styleKindsHint, style: TextStyle(fontSize: 12.5, color: palette.textSoft)),
              const SizedBox(height: AppSpacing.xs),
              for (final (kind, label) in <(String, String)>[('free', t.kindFree), ('plan', t.kindPlan), ('paid', t.kindPaid)])
                Row(
                  children: <Widget>[
                    Expanded(child: _slider(label, _kinds[kind]!, (value) => _kinds[kind] = value)),
                    SizedBox(
                      width: 92,
                      child: Text(
                        _kinds[kind]! < 0.01 ? t.styleKindOff : '${(_kinds[kind]! * 100).round()} %',
                        textAlign: TextAlign.end,
                        style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                      ),
                    ),
                  ],
                ),
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
