import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_picker.dart';
import 'main_shared.dart';
import 'main_theme.dart';

/// A saved set of models and routings ("DeepSeek + Qwen Max") that every
/// model selector can add in one tap.
class ModelPreset {
  const ModelPreset({required this.id, required this.name, required this.models});

  factory ModelPreset.fromJson(Map<String, dynamic> json) =>
      ModelPreset(id: '${json['id']}', name: '${json['name']}', models: (json['models'] as List).cast<String>());

  final String id;
  final String name;
  final List<String> models;

  Map<String, Object> toJson() => <String, Object>{'id': id, 'name': name, 'models': models};
}

/// The person's presets, loaded once and shared by every selector on screen.
class ModelPresets {
  ModelPresets._();

  static final ValueNotifier<List<ModelPreset>> current = ValueNotifier<List<ModelPreset>>(<ModelPreset>[]);
  static Future<void>? _loading;

  static Future<void> load(NeoRouterController controller) => _loading ??= () async {
    try {
      current.value = (await controller.api.modelPresets()).map(ModelPreset.fromJson).toList();
    } catch (_) {
      _loading = null;
    }
  }();

  static Future<void> save(NeoRouterController controller, List<ModelPreset> presets) async {
    await controller.api.saveModelPresets(presets.map((preset) => preset.toJson()).toList());
    current.value = presets;
  }
}

/// Preset chips for a selection: tapping one adds its models (or removes
/// them when all are already in), the last chip saves the selection as a
/// new preset. Used inside the model picker and next to every model field.
class PresetBar extends StatefulWidget {
  const PresetBar({super.key, required this.controller, required this.selected, required this.onChanged});

  final NeoRouterController controller;
  final List<String> selected;
  final ValueChanged<List<String>> onChanged;

  @override
  State<PresetBar> createState() => _PresetBarState();
}

class _PresetBarState extends State<PresetBar> {
  @override
  void initState() {
    super.initState();
    ModelPresets.load(widget.controller);
  }

  bool _applied(ModelPreset preset) => preset.models.every(widget.selected.contains);

  void _toggle(ModelPreset preset) {
    final next = _applied(preset)
        ? widget.selected.where((id) => !preset.models.contains(id)).toList()
        : <String>{...widget.selected, ...preset.models}.toList();
    widget.onChanged(next);
  }

  Future<void> _saveCurrent() async {
    final t = AppL10n.of(context);
    final name = TextEditingController();
    final saved = await showDialog<String>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.presetSave),
        content: TextField(
          controller: name,
          autofocus: true,
          decoration: InputDecoration(labelText: t.presetName, hintText: t.presetNameHint),
          onSubmitted: (value) => Navigator.of(dialogContext).pop(value),
        ),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(name.text), child: Text(t.save)),
        ],
      ),
    );
    name.dispose();
    if (saved == null || saved.trim().isEmpty || !mounted) return;
    final preset = ModelPreset(id: DateTime.now().microsecondsSinceEpoch.toRadixString(36), name: saved.trim(), models: List<String>.of(widget.selected));
    await _store(<ModelPreset>[...ModelPresets.current.value, preset]);
  }

  Future<void> _delete(ModelPreset preset) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.delete, message: t.presetDelete(preset.name), confirmLabel: t.delete, cancelLabel: t.cancel);
    if (confirmed && mounted) await _store(ModelPresets.current.value.where((item) => item.id != preset.id).toList());
  }

  Future<void> _store(List<ModelPreset> presets) async {
    try {
      await ModelPresets.save(widget.controller, presets);
    } catch (error) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text('$error')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ValueListenableBuilder<List<ModelPreset>>(
      valueListenable: ModelPresets.current,
      builder: (context, presets, _) {
        final exists = presets.any((preset) => preset.models.length == widget.selected.length && _applied(preset));
        final canSave = widget.selected.isNotEmpty && !exists;
        if (presets.isEmpty && !canSave) return const SizedBox.shrink();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(t.presetsTitle.toUpperCase(), style: sectionEyebrowStyle(palette)),
            const SizedBox(height: 6),
            Wrap(
              spacing: 6,
              runSpacing: 6,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                // Tap adds or removes the preset's models; long-press deletes it.
                for (final preset in presets)
                  Tooltip(
                    message: '${preset.models.map(shortModelName).join(', ')}\n${t.presetApplyHint}',
                    child: GestureDetector(
                      onLongPress: () => _delete(preset),
                      child: FilterChip(
                        visualDensity: VisualDensity.compact,
                        label: Text(preset.name),
                        selected: _applied(preset),
                        selectedColor: palette.accentSoft,
                        side: BorderSide(color: _applied(preset) ? palette.accent.withValues(alpha: 0.5) : palette.border),
                        onSelected: (_) => _toggle(preset),
                      ),
                    ),
                  ),
                if (canSave)
                  TextButton.icon(
                    style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
                    onPressed: _saveCurrent,
                    icon: const Icon(Icons.add_rounded, size: 16),
                    label: Text(t.presetSave),
                  ),
              ],
            ),
          ],
        );
      },
    );
  }
}

/// The one model field for forms: the chosen models and routings as chips,
/// presets, a button that opens the picker, and (with [patterns]) a box for
/// wildcards like "groq/*". An empty selection means any model.
class ModelSelectionField extends StatefulWidget {
  const ModelSelectionField({
    super.key,
    required this.controller,
    required this.value,
    required this.onChanged,
    this.title,
    this.emptyHint,
    this.routes = const <(String, String)>[],
    this.capabilities = const <String>[],
    this.patterns = true,
  });

  final NeoRouterController controller;
  final List<String> value;
  final ValueChanged<List<String>> onChanged;
  final String? title;
  final String? emptyHint;

  /// Routings offered in the picker above the models (id, label).
  final List<(String, String)> routes;

  /// Kinds of model the picker offers; empty means chat.
  final List<String> capabilities;
  final bool patterns;

  @override
  State<ModelSelectionField> createState() => _ModelSelectionFieldState();
}

class _ModelSelectionFieldState extends State<ModelSelectionField> {
  final TextEditingController _pattern = TextEditingController();

  @override
  void dispose() {
    _pattern.dispose();
    super.dispose();
  }

  String _label(String id) {
    for (final (route, label) in widget.routes) {
      if (route == id) return label;
    }
    return id.contains('*') ? id : shortModelName(id);
  }

  Future<void> _pick() async {
    final wildcards = widget.value.where((item) => item.contains('*'));
    final picked = await showModelPicker(
      context,
      controller: widget.controller,
      selected: widget.value.where((item) => !item.contains('*')).toList(),
      multi: true,
      routes: widget.routes,
      capabilities: widget.capabilities,
    );
    if (picked != null) widget.onChanged(<String>{...wildcards, ...picked}.toList());
  }

  void _addPattern() {
    final value = _pattern.text.trim();
    if (value.isEmpty) return;
    if (!widget.value.contains(value)) widget.onChanged(<String>[...widget.value, value]);
    _pattern.clear();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(child: Text(widget.title ?? t.modelsTitle, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text))),
            if (widget.value.isNotEmpty) TextButton(onPressed: () => widget.onChanged(<String>[]), child: Text(t.modelsClear)),
          ],
        ),
        const SizedBox(height: 6),
        // The selection itself, in one bordered box that opens the picker.
        InkWell(
          onTap: _pick,
          borderRadius: BorderRadius.circular(10),
          child: Container(
            constraints: const BoxConstraints(minHeight: 48),
            padding: const EdgeInsets.fromLTRB(10, 8, 8, 8),
            decoration: BoxDecoration(
              color: palette.surface,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(color: palette.border),
            ),
            child: Row(
              children: <Widget>[
                Expanded(
                  child: widget.value.isEmpty
                      ? Text(widget.emptyHint?.isNotEmpty == true ? widget.emptyHint! : t.modelsAny, style: TextStyle(fontSize: 13, color: palette.textMuted))
                      : ConstrainedBox(
                          constraints: BoxConstraints(maxHeight: math.max(120, MediaQuery.sizeOf(context).height * 0.25)),
                          child: SingleChildScrollView(
                            child: Wrap(
                              spacing: 6,
                              runSpacing: 6,
                              children: <Widget>[
                                for (final id in widget.value)
                                  InputChip(
                                    visualDensity: VisualDensity.compact,
                                    label: Tooltip(message: id, child: Text(_label(id))),
                                    onDeleted: () => widget.onChanged(widget.value.where((item) => item != id).toList()),
                                    deleteIcon: const Icon(Icons.close_rounded, size: 14),
                                  ),
                              ],
                            ),
                          ),
                        ),
                ),
                const SizedBox(width: 6),
                Tooltip(message: t.pickModels, child: Icon(Icons.add_circle_outline_rounded, color: palette.accent)),
              ],
            ),
          ),
        ),
        const SizedBox(height: 12),
        PresetBar(controller: widget.controller, selected: widget.value, onChanged: widget.onChanged),
        if (widget.patterns) ...<Widget>[
          const SizedBox(height: 8),
          TextField(
            controller: _pattern,
            onSubmitted: (_) => _addPattern(),
            style: const TextStyle(fontSize: 13),
            decoration: InputDecoration(
              isDense: true,
              prefixIcon: const Icon(Icons.filter_alt_outlined, size: 16),
              hintText: t.routingGroupPatternHint,
              suffixIcon: IconButton(icon: const Icon(Icons.keyboard_return_rounded, size: 16), tooltip: t.routingGroupAddPattern, onPressed: _addPattern),
            ),
          ),
        ],
      ],
    );
  }
}
