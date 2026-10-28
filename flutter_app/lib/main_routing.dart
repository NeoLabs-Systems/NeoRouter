import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_models.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> presetOrder = <String>['reliable', 'fast', 'balanced', 'best', 'smart', 'cheap'];

String presetLabel(AppL10n t, String id) => switch (id) {
      'reliable' => t.presetReliable,
      'fast' => t.presetFast,
      'balanced' => t.presetBalanced,
      'best' => t.presetBest,
      'smart' => t.presetSmart,
      'cheap' => t.presetCheap,
      _ => t.presetCustom,
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
  RoutingWeights _weights = RoutingWeights(intelligence: 0.3, speed: 0.2, reliability: 0.3, cost: 0.2);
  final TextEditingController _name = TextEditingController();
  final TextEditingController _maxPrice = TextEditingController();
  final TextEditingController _exclude = TextEditingController();
  double _minIntelligence = 0;
  double _exploration = 0.08;
  bool _adaptive = true;

  final TextEditingController _prompt = TextEditingController(text: 'Explain quicksort in two sentences.');
  bool _previewFree = false;
  bool _previewTools = false;
  bool _previewVision = false;
  Map<String, dynamic>? _preview;
  bool _previewing = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _name.dispose();
    _maxPrice.dispose();
    _exclude.dispose();
    _prompt.dispose();
    super.dispose();
  }

  RoutingProfile? get _current => _profiles.where((item) => item.id == _selected).firstOrNull;
  bool get _isCustom => _current == null || !_current!.preset;

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final profiles = await widget.controller.api.profiles();
      setState(() => _profiles = profiles);
      _select(_selected);
      await _runPreview();
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _select(String id) {
    final profile = _profiles.where((item) => item.id == id).firstOrNull;
    setState(() {
      _selected = id;
      if (profile != null) {
        _weights = profile.weights;
        final constraints = profile.constraints;
        _name.text = profile.preset ? '' : profile.name;
        _minIntelligence = (constraints['minIntelligence'] as num?)?.toDouble() ?? 0;
        _exploration = (constraints['exploration'] as num?)?.toDouble() ?? 0.08;
        _adaptive = constraints['adaptive'] != false;
        _maxPrice.text = constraints['maxPriceOut']?.toString() ?? '';
        _exclude.text = ((constraints['excludeProviders'] as List?) ?? const <Object>[]).join(', ');
      }
    });
  }

  Map<String, Object?> _constraints() => <String, Object?>{
        'minIntelligence': _minIntelligence > 0 ? _minIntelligence : null,
        'maxPriceOut': double.tryParse(_maxPrice.text.trim().replaceAll(',', '.')),
        'excludeProviders': _exclude.text.split(',').map((item) => item.trim()).where((item) => item.isNotEmpty).toList(),
        'adaptive': _adaptive,
        'exploration': _exploration,
      };

  Future<void> _saveProfile({required bool asNew}) async {
    final t = AppL10n.of(context);
    try {
      final name = _name.text.trim().isEmpty ? t.presetCustom : _name.text.trim();
      final profile = asNew || _current == null || _current!.preset
          ? await widget.controller.api.createProfile(name, _weights, _constraints())
          : await widget.controller.api.updateProfileWeights(_selected, name: name, weights: _weights, constraints: _constraints());
      _selected = profile.id;
      await _load();
      if (mounted) showSnack(context, t.saved);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  Future<void> _deleteProfile() async {
    await widget.controller.api.deleteProfile(_selected);
    _selected = 'balanced';
    await _load();
  }

  Future<void> _runPreview() async {
    setState(() => _previewing = true);
    try {
      final usePreset = !_isCustom || _current != null;
      final result = await widget.controller.api.preview(<String, Object?>{
        'profile': usePreset ? _selected : 'balanced',
        'mode': _previewFree ? 'free_only' : 'both',
        'prompt': _prompt.text,
        'tools': _previewTools,
        'vision': _previewVision,
      });
      setState(() => _preview = result);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _previewing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(title: t.routingTitle, description: t.routingDescription),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: LayoutBuilder(
            builder: (context, constraints) {
              final editor = _editor(context);
              final preview = _previewCard(context);
              if (constraints.maxWidth < 1000) return Column(children: <Widget>[editor, const SizedBox(height: AppSpacing.md), preview]);
              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(child: editor),
                  const SizedBox(width: AppSpacing.md),
                  Expanded(child: preview),
                ],
              );
            },
          ),
        ),
      ],
    );
  }

  Widget _editor(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final customProfiles = _profiles.where((item) => !item.preset).toList();
    final presetIndex = presetOrder.indexOf(_selected);
    final sliderIndex = presetIndex >= 0 ? presetIndex : presetOrder.length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SectionCard(
          title: t.presetSlider,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Slider(
                value: sliderIndex.toDouble(),
                min: 0,
                max: presetOrder.length.toDouble(),
                divisions: presetOrder.length,
                label: sliderIndex < presetOrder.length ? presetLabel(t, presetOrder[sliderIndex]) : t.presetCustom,
                onChanged: (value) {
                  final index = value.round();
                  if (index < presetOrder.length) {
                    _select(presetOrder[index]);
                  } else {
                    setState(() => _selected = customProfiles.isNotEmpty ? customProfiles.first.id : '__new__');
                    if (customProfiles.isNotEmpty) _select(customProfiles.first.id);
                  }
                },
                onChangeEnd: (_) => _runPreview(),
              ),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                alignment: WrapAlignment.center,
                children: <Widget>[
                  for (var index = 0; index <= presetOrder.length; index++)
                    ChoiceChip(
                      label: Text(index < presetOrder.length ? presetLabel(t, presetOrder[index]) : t.presetCustom),
                      selected: index == sliderIndex,
                      onSelected: (_) {
                        if (index < presetOrder.length) {
                          _select(presetOrder[index]);
                        } else if (customProfiles.isNotEmpty) {
                          _select(customProfiles.first.id);
                        } else {
                          setState(() => _selected = '__new__');
                        }
                        _runPreview();
                      },
                    ),
                ],
              ),
              if (_isCustom && customProfiles.isNotEmpty) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: <Widget>[
                    for (final profile in customProfiles)
                      ChoiceChip(label: Text(profile.name), selected: _selected == profile.id, onSelected: (_) {
                        _select(profile.id);
                        _runPreview();
                      }),
                    ChoiceChip(label: Text('+ ${t.presetCustom}'), selected: _selected == '__new__', onSelected: (_) => setState(() => _selected = '__new__')),
                  ],
                ),
              ],
              if (!_isCustom || _current != null) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                Text(t.profileUsage(_selected), style: TextStyle(color: palette.textMuted, fontSize: 12)),
              ],
            ],
          ),
        ),
        const SizedBox(height: AppSpacing.md),
        SectionCard(
          title: t.weights,
          child: Column(
            children: <Widget>[
              _weightSlider(t.intelligence, _weights.intelligence, (value) => _weights.intelligence = value),
              _weightSlider(t.speed, _weights.speed, (value) => _weights.speed = value),
              _weightSlider(t.reliability, _weights.reliability, (value) => _weights.reliability = value),
              _weightSlider(t.cost, _weights.cost, (value) => _weights.cost = value),
            ],
          ),
        ),
        if (_isCustom) ...<Widget>[
          const SizedBox(height: AppSpacing.md),
          SectionCard(
            title: t.constraints,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                TextField(controller: _name, decoration: InputDecoration(labelText: t.profileName)),
                const SizedBox(height: AppSpacing.sm),
                _labeledSlider(t.minIntelligence, _minIntelligence, (value) => setState(() => _minIntelligence = value), max: 0.9),
                _labeledSlider(t.exploration, _exploration, (value) => setState(() => _exploration = value), min: 0.01, max: 0.3, hint: t.explorationHint),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  value: _adaptive,
                  onChanged: (value) => setState(() => _adaptive = value),
                  title: Text(t.adaptive),
                  subtitle: Text(t.adaptiveHint, style: TextStyle(color: palette.textMuted, fontSize: 12)),
                ),
                TextField(controller: _maxPrice, keyboardType: const TextInputType.numberWithOptions(decimal: true), decoration: InputDecoration(labelText: t.maxPriceOut)),
                const SizedBox(height: AppSpacing.sm),
                TextField(controller: _exclude, decoration: InputDecoration(labelText: t.excludeProviders)),
                const SizedBox(height: AppSpacing.md),
                Wrap(
                  spacing: AppSpacing.sm,
                  runSpacing: AppSpacing.sm,
                  children: <Widget>[
                    if (_current != null && !_current!.preset) ...<Widget>[
                      FilledButton(onPressed: () => _saveProfile(asNew: false), child: Text(t.updateProfile)),
                      OutlinedButton(onPressed: () => _saveProfile(asNew: true), child: Text(t.saveProfile)),
                      TextButton(onPressed: _deleteProfile, child: Text(t.delete)),
                    ] else
                      FilledButton(onPressed: () => _saveProfile(asNew: true), child: Text(t.saveProfile)),
                  ],
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }

  Widget _weightSlider(String label, double value, ValueChanged<double> assign) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      children: <Widget>[
        SizedBox(width: 120, child: Text(label, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13))),
        Expanded(
          child: Slider(
            value: value.clamp(0, 1),
            onChanged: _isCustom ? (next) => setState(() => assign(next)) : null,
          ),
        ),
        SizedBox(width: 40, child: Text('${(value * 100).round()}', textAlign: TextAlign.end, style: TextStyle(color: palette.textSoft, fontSize: 12))),
      ],
    );
  }

  Widget _labeledSlider(String label, double value, ValueChanged<double> onChanged, {double min = 0, double max = 1, String? hint}) {
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(child: Text(label, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13))),
            Text(value.toStringAsFixed(2), style: TextStyle(color: palette.textSoft, fontSize: 12)),
          ],
        ),
        Slider(value: value.clamp(min, max), min: min, max: max, onChanged: onChanged),
        if (hint != null) Text(hint, style: TextStyle(color: palette.textMuted, fontSize: 12)),
      ],
    );
  }

  Widget _previewCard(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final candidates = ((_preview?['candidates'] as List?) ?? const <Object>[]).cast<Map>();
    return SectionCard(
      title: t.previewTitle,
      trailing: _previewing ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(t.previewHint, style: TextStyle(color: palette.textSoft, fontSize: 13)),
          const SizedBox(height: AppSpacing.sm),
          TextField(controller: _prompt, minLines: 1, maxLines: 4, decoration: InputDecoration(labelText: t.previewPrompt)),
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              FilterChip(label: Text(t.previewFreeOnly), selected: _previewFree, onSelected: (value) => setState(() => _previewFree = value)),
              FilterChip(label: Text(t.previewTools), selected: _previewTools, onSelected: (value) => setState(() => _previewTools = value)),
              FilterChip(label: Text(t.previewVision), selected: _previewVision, onSelected: (value) => setState(() => _previewVision = value)),
              OutlinedButton.icon(onPressed: _previewing ? null : _runPreview, icon: const Icon(Icons.play_arrow_rounded, size: 18), label: Text(t.runPreview)),
            ],
          ),
          const SizedBox(height: AppSpacing.md),
          for (final candidate in candidates) _CandidateRow(candidate: candidate),
        ],
      ),
    );
  }
}

class _CandidateRow extends StatelessWidget {
  const _CandidateRow({required this.candidate});

  final Map candidate;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final rejected = candidate['rejected'] as String?;
    final features = (candidate['features'] as Map?) ?? const <String, Object>{};
    final score = (candidate['score'] as num?)?.toDouble();
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Opacity(
        opacity: rejected == null ? 1 : 0.55,
        child: Row(
          children: <Widget>[
            SizedBox(
              width: 48,
              child: Text(
                score == null ? '—' : score.clamp(0, 9.99).toStringAsFixed(2),
                style: TextStyle(fontWeight: FontWeight.w800, color: rejected == null ? palette.accentStrong : palette.textMuted),
              ),
            ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text('${candidate['id']}', overflow: TextOverflow.ellipsis, style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5, fontWeight: FontWeight.w600)),
                  if (rejected != null)
                    Text(t.excluded(routingReasonLabel(t, rejected)), style: TextStyle(color: palette.error, fontSize: 12))
                  else
                    Text(
                      '${t.intelligence} ${_pct(features['intelligence'])} · ${t.speed} ${_pct(features['speed'])} · ${t.reliability} ${_pct(features['reliability'])} · ${t.cost} ${_pct(features['cost'])}',
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                ],
              ),
            ),
            if (candidate['isFree'] == true) MetaPill(label: t.free, tone: PillTone.success),
          ],
        ),
      ),
    );
  }

  String _pct(Object? value) => value is num ? '${(value * 100).round()}' : '—';
}
