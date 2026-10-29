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

/// A task kind routing recognises (server: services/routing/task/kinds.js).
String taskLabel(AppL10n t, String? id) => switch (id) {
      'chat' => t.taskChat,
      'coding' => t.taskCoding,
      'math' => t.taskMath,
      'reasoning' => t.taskReasoning,
      'writing' => t.taskWriting,
      'translation' => t.taskTranslation,
      'summarization' => t.taskSummarization,
      'extraction' => t.taskExtraction,
      'agent' => t.taskAgent,
      'vision' => t.taskVision,
      'long_context' => t.taskLongContext,
      'knowledge' => t.taskKnowledge,
      _ => t.taskGeneral,
    };

String difficultyLabel(AppL10n t, num difficulty) => difficulty < 0.3 ? t.taskEasy : difficulty > 0.5 ? t.taskHard : t.taskMedium;

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
  final TextEditingController _previewPrompt = TextEditingController();
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
    _previewPrompt.dispose();
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
        'prompt': _previewPrompt.text.trim().isEmpty ? t.previewDefaultPrompt : _previewPrompt.text.trim(),
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
    final modelName = 'auto:${profile.slug}';
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
    final task = _preview?['task'] is Map ? Map<String, dynamic>.from(_preview!['task'] as Map) : null;
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
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(t.previewPromptLabel, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
              const SizedBox(height: 2),
              Text(t.previewPromptHint, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.4)),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: <Widget>[
                  Expanded(
                    child: TextField(
                      controller: _previewPrompt,
                      minLines: 1,
                      maxLines: 4,
                      decoration: InputDecoration(isDense: true, hintText: t.previewDefaultPrompt),
                      onSubmitted: (_) => _runPreview(),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  OutlinedButton(onPressed: _previewing ? null : _runPreview, child: Text(t.previewPromptTry)),
                ],
              ),
              if (task != null) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                Row(
                  children: <Widget>[
                    Icon(Icons.label_outline_rounded, size: 16, color: palette.accent),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        t.taskRecognised(taskLabel(t, task['primary'] as String?), difficultyLabel(t, (task['difficulty'] as num?) ?? 0.3)),
                        style: TextStyle(fontSize: 13, color: palette.text, fontWeight: FontWeight.w600),
                      ),
                    ),
                  ],
                ),
              ],
            ],
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

// Plain-language steps for the fine-tuning sliders, mapped to the values the
// router uses.
const List<double> _qualityFloors = <double>[0, 0.3, 0.5, 0.7];
const List<double?> _priceCaps = <double?>[null, 20, 5, 1, 0.3];
const List<double> _varietySteps = <double>[0.02, 0.08, 0.2];
// "Prefer", "If needed", "Never" for a kind of model.
const List<double> _kindSteps = <double>[1, 0.25, 0];

int _nearest(List<double?> steps, double? value) {
  if (value == null) return steps.contains(null) ? steps.indexOf(null) : 0;
  var best = 0;
  for (var index = 0; index < steps.length; index++) {
    final step = steps[index];
    if (step == null) continue;
    final current = steps[best];
    if (current == null || (step - value).abs() < (current - value).abs()) best = index;
  }
  return best;
}

class _StyleEditorState extends State<_StyleEditor> {
  late final RoutingWeights _weights = widget.profile?.weights.copy() ?? RoutingWeights(intelligence: 0.6, speed: 0.4, reliability: 0.6, cost: 0.4);
  late final Map<String, dynamic> _constraints = widget.profile?.constraints ?? <String, dynamic>{};
  late final TextEditingController _name = TextEditingController(text: widget.profile?.name ?? '');
  late int _quality = _nearest(_qualityFloors, (_constraints['minIntelligence'] as num?)?.toDouble() ?? 0);
  late int _price = _nearest(_priceCaps, (_constraints['maxPriceOut'] as num?)?.toDouble());
  late int _variety = _nearest(_varietySteps, (_constraints['exploration'] as num?)?.toDouble() ?? 0.08);
  late bool _adaptive = _constraints['adaptive'] != false;
  late final Set<String> _skipped = <String>{...((_constraints['excludeProviders'] as List?) ?? const <Object>[]).map((item) => '$item')};
  late final Map<String, int> _kinds = <String, int>{
    for (final kind in const <String>['free', 'plan', 'paid'])
      kind: _nearest(_kindSteps, ((_constraints['kinds'] as Map?)?[kind] as num?)?.toDouble() ?? 1),
  };
  List<ProviderAccount> _accounts = <ProviderAccount>[];
  bool _more = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    widget.controller.api.accounts().then((accounts) {
      if (mounted) setState(() => _accounts = accounts);
    }).catchError((Object _) {});
  }

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Map<String, Object?> _constraintBody() => <String, Object?>{
        'minIntelligence': _qualityFloors[_quality] > 0 ? _qualityFloors[_quality] : null,
        'maxPriceOut': _priceCaps[_price],
        'excludeProviders': _skipped.toList(),
        'adaptive': _adaptive,
        'exploration': _varietySteps[_variety],
        // Every kind preferred is the same as no preference.
        'kinds': _kinds.values.every((step) => step == 0)
            ? null
            : <String, double>{for (final entry in _kinds.entries) entry.key: _kindSteps[entry.value]},
      };

  Future<void> _save() async {
    final t = AppL10n.of(context);
    if (_kinds.values.every((step) => _kindSteps[step] == 0)) {
      setState(() => _error = t.pickOneKind);
      return;
    }
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

  String _level(AppL10n t, double value) => switch ((value * 3).round()) {
        0 => t.styleLevel0,
        1 => t.styleLevel1,
        2 => t.styleLevel2,
        _ => t.styleLevel3,
      };

  Widget _section(String title, {String? hint, required List<Widget> children}) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(title, style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5, color: palette.text)),
          if (hint != null) ...<Widget>[
            const SizedBox(height: 3),
            Text(hint, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.4)),
          ],
          const SizedBox(height: AppSpacing.sm),
          ...children,
        ],
      ),
    );
  }

  // One priority: icon, name, what it means, and how much it counts in words.
  Widget _priority(IconData icon, String label, String hint, double value, ValueChanged<double> onChanged) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        children: <Widget>[
          Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(10)),
            child: Icon(icon, size: 18, color: palette.textSoft),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Row(
                  children: <Widget>[
                    Expanded(child: Text(label, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text))),
                    Text(_level(t, value), style: TextStyle(fontSize: 12.5, color: palette.accent, fontWeight: FontWeight.w600)),
                  ],
                ),
                Text(hint, style: TextStyle(fontSize: 12, color: palette.textMuted)),
                SliderTheme(
                  data: SliderTheme.of(context).copyWith(trackHeight: 3, overlayShape: SliderComponentShape.noOverlay),
                  child: Slider(value: value.clamp(0, 1), divisions: 3, onChanged: (next) => setState(() => onChanged(next))),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  // A slider over named steps: the current step's name above it.
  Widget _steps(String label, List<String> names, int index, ValueChanged<int> onChanged, {String? detail}) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(child: Text(label, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text))),
              Text(names[index], style: TextStyle(fontSize: 12.5, color: palette.accent, fontWeight: FontWeight.w600)),
            ],
          ),
          SliderTheme(
            data: SliderTheme.of(context).copyWith(trackHeight: 3, overlayShape: SliderComponentShape.noOverlay),
            child: Slider(
              value: index.toDouble(),
              min: 0,
              max: (names.length - 1).toDouble(),
              divisions: names.length - 1,
              onChanged: (next) => setState(() => onChanged(next.round())),
            ),
          ),
          if (detail != null) Text(detail, style: TextStyle(fontSize: 12, color: palette.textMuted)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final price = _priceCaps[_price];
    final kindLabels = <(String, String)>[('free', t.kindFree), ('plan', t.kindPlan), ('paid', t.kindPaid)];
    return AlertDialog(
      title: Text(widget.profile == null ? t.createStyle : t.editStyle),
      content: SizedBox(
        width: 540,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _name, autofocus: widget.profile == null, decoration: InputDecoration(labelText: t.profileName)),
              const SizedBox(height: AppSpacing.lg),
              _section(
                t.stylePrioritiesTitle,
                hint: t.presetCheaperNote,
                children: <Widget>[
                  _priority(Icons.psychology_outlined, t.stylePriorityQuality, t.stylePriorityQualityHint, _weights.intelligence, (value) => _weights.intelligence = value),
                  _priority(Icons.bolt_outlined, t.stylePrioritySpeed, t.stylePrioritySpeedHint, _weights.speed, (value) => _weights.speed = value),
                  _priority(Icons.verified_outlined, t.stylePriorityReliability, t.stylePriorityReliabilityHint, _weights.reliability, (value) => _weights.reliability = value),
                  _priority(Icons.savings_outlined, t.stylePriorityCost, t.stylePriorityCostHint, _weights.cost, (value) => _weights.cost = value),
                ],
              ),
              _section(
                t.styleKindsTitle,
                hint: t.styleKindsHint,
                children: <Widget>[
                  for (final (kind, label) in kindLabels)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: Row(
                        children: <Widget>[
                          Expanded(child: Text(label, style: TextStyle(color: palette.text))),
                          SegmentedButton<int>(
                            showSelectedIcon: false,
                            style: const ButtonStyle(visualDensity: VisualDensity.compact),
                            segments: <ButtonSegment<int>>[
                              ButtonSegment<int>(value: 0, label: Text(t.styleKindPrefer)),
                              ButtonSegment<int>(value: 1, label: Text(t.styleKindIfNeeded)),
                              ButtonSegment<int>(value: 2, label: Text(t.styleKindNever)),
                            ],
                            selected: <int>{_kinds[kind]!},
                            onSelectionChanged: (value) => setState(() => _kinds[kind] = value.first),
                          ),
                        ],
                      ),
                    ),
                ],
              ),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _more = !_more),
                  icon: Icon(_more ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.styleMoreTitle),
                ),
              ),
              if (_more) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                _steps(t.styleQualityFloor, <String>[t.styleQualityAny, t.styleQualityDecent, t.styleQualityGood, t.styleQualityTop], _quality, (value) => _quality = value),
                _steps(
                  t.stylePriceCap,
                  <String>[t.stylePriceAny, t.stylePricePremium, t.stylePriceMid, t.stylePriceCheap, t.stylePriceVeryCheap],
                  _price,
                  (value) => _price = value,
                  detail: price == null ? null : t.stylePriceDetail(formatUsd(price)),
                ),
                _steps(t.styleVariety, <String>[t.styleVarietyLow, t.styleVarietyMid, t.styleVarietyHigh], _variety, (value) => _variety = value),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  value: _adaptive,
                  onChanged: (value) => setState(() => _adaptive = value),
                  title: Text(t.styleAdaptive, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text, fontSize: 14)),
                  subtitle: Text(t.styleAdaptiveHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                ),
                if (_accounts.isNotEmpty) ...<Widget>[
                  const SizedBox(height: AppSpacing.sm),
                  Text(t.styleSkipProviders, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                  Text(t.styleSkipProvidersHint, style: TextStyle(fontSize: 12, color: palette.textMuted)),
                  const SizedBox(height: AppSpacing.xs),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: <Widget>[
                      for (final account in _accounts)
                        FilterChip(
                          label: Text(account.label),
                          selected: _skipped.contains(account.slug),
                          onSelected: (on) => setState(() => on ? _skipped.add(account.slug) : _skipped.remove(account.slug)),
                        ),
                    ],
                  ),
                ],
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
