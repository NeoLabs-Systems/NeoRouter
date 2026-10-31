import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_models.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

/// Picks one model ([multi] false, returns its id) or several (returns their
/// ids) from the catalog for [capability], so nobody has to type model ids.
/// Search matches name, id and provider; filters narrow by price kind and
/// what the model can do. [routes] (model id, label) are offered above the
/// models, for picking routings such as "auto:fast". Returns null when cancelled.
Future<List<String>?> showModelPicker(
  BuildContext context, {
  required NeoRouterController controller,
  String capability = 'chat',
  List<String> selected = const <String>[],
  bool multi = false,
  List<(String, String)> routes = const <(String, String)>[],
  List<String> capabilities = const <String>[],
}) {
  final mobile = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
  return showDialog<List<String>>(
    context: context,
    builder: (_) => Dialog(
      insetPadding: mobile ? const EdgeInsets.all(12) : const EdgeInsets.all(40),
      clipBehavior: Clip.antiAlias,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 620, maxHeight: 720),
        child: _ModelPicker(controller: controller, capability: capability, selected: selected, multi: multi, routes: routes, capabilities: capabilities),
      ),
    ),
  );
}

enum _Kind { all, free, plan, paid }

class _ModelPicker extends StatefulWidget {
  const _ModelPicker({required this.controller, required this.capability, required this.selected, required this.multi, required this.routes, this.capabilities = const <String>[]});

  final NeoRouterController controller;
  final String capability;
  final List<String> selected;
  final bool multi;
  final List<(String, String)> routes;

  /// Several kinds to choose from (a kind filter appears); empty: just [capability].
  final List<String> capabilities;

  @override
  State<_ModelPicker> createState() => _ModelPickerState();
}

class _ModelPickerState extends State<_ModelPicker> {
  final TextEditingController _search = TextEditingController();
  late final Set<String> _chosen = widget.selected.toSet();
  List<CatalogModel> _models = <CatalogModel>[];
  bool _loading = true;
  String? _error;
  _Kind _kind = _Kind.all;
  bool _tools = false;
  bool _vision = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final kinds = widget.capabilities.isEmpty ? <String>[widget.capability] : widget.capabilities;
      final lists = await Future.wait(kinds.map((kind) => widget.controller.api.models(capability: kind)));
      if (mounted) setState(() => _models = lists.expand((list) => list).where((model) => model.enabled).toList());
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  // The kind shown when several are offered (null: all of them).
  String? _capabilityFilter;

  bool get _isChat => (_capabilityFilter ?? (widget.capabilities.isEmpty ? widget.capability : null)) == 'chat';

  List<CatalogModel> _visible() {
    final words = _search.text.toLowerCase().split(RegExp(r'\s+')).where((word) => word.isNotEmpty);
    return _models.where((model) {
      if (_capabilityFilter != null && model.capability != _capabilityFilter) return false;
      final haystack = '${model.name} ${model.id} ${model.providerSlug}'.toLowerCase();
      if (!words.every(haystack.contains)) return false;
      final kindOk = switch (_kind) {
        _Kind.all => true,
        _Kind.free => model.isFree,
        _Kind.plan => model.isPlan,
        _Kind.paid => !model.isFree && !model.isPlan,
      };
      return kindOk && (!_tools || model.tools == true) && (!_vision || model.vision == true);
    }).toList()
      // Free first, then by name, so the cheapest fits are on top.
      ..sort((a, b) {
        int rank(CatalogModel model) => model.isFree ? 0 : (model.isPlan ? 1 : 2);
        final byRank = rank(a).compareTo(rank(b));
        return byRank != 0 ? byRank : a.name.toLowerCase().compareTo(b.name.toLowerCase());
      });
  }

  // Routings are not models: price and capability filters hide them.
  List<(String, String)> _visibleRoutes() {
    if (_kind != _Kind.all || _tools || _vision) return const <(String, String)>[];
    final words = _search.text.toLowerCase().split(RegExp(r'\s+')).where((word) => word.isNotEmpty);
    return widget.routes.where((route) => words.every('${route.$1} ${route.$2}'.toLowerCase().contains)).toList();
  }

  void _tap(String id) {
    if (!widget.multi) {
      Navigator.of(context).pop(<String>[id]);
      return;
    }
    setState(() => _chosen.contains(id) ? _chosen.remove(id) : _chosen.add(id));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final visible = _visible();
    final routes = _visibleRoutes();
    // Grouped by provider, providers in order of their first (best) model.
    final groups = <String, List<CatalogModel>>{};
    for (final model in visible) {
      groups.putIfAbsent(model.providerSlug, () => <CatalogModel>[]).add(model);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 18, 12, 0),
          child: Row(
            children: <Widget>[
              Expanded(child: Text(widget.multi ? t.pickModels : t.pickModel, style: displayTitleStyle(palette, size: 20))),
              IconButton(tooltip: t.close, onPressed: () => Navigator.of(context).pop(), icon: const Icon(Icons.close_rounded, size: 20)),
            ],
          ),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 10, 20, 0),
          child: TextField(
            controller: _search,
            autofocus: MediaQuery.sizeOf(context).width >= AppBreakpoints.mobile,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(isDense: true, prefixIcon: const Icon(Icons.search_rounded, size: 18), hintText: t.searchModels),
          ),
        ),
        if (widget.capabilities.length > 1)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 10, 20, 0),
            child: SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: <Widget>[
                  for (final kind in <String?>[null, ...widget.capabilities])
                    Padding(
                      padding: const EdgeInsets.only(right: 6),
                      child: ChoiceChip(
                        avatar: kind == null ? null : Icon(capabilityInfo(kind).icon, size: 15),
                        label: Text(kind == null ? t.pickModelAllKinds : capabilityInfo(kind).label(t)),
                        selected: _capabilityFilter == kind,
                        onSelected: (_) => setState(() => _capabilityFilter = kind),
                        visualDensity: VisualDensity.compact,
                      ),
                    ),
                ],
              ),
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 10, 20, 6),
          child: Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              for (final (kind, label) in <(_Kind, String)>[(_Kind.all, t.filterAll), (_Kind.free, t.free), (_Kind.plan, t.includedInPlan), (_Kind.paid, t.paid)])
                ChoiceChip(label: Text(label), selected: _kind == kind, onSelected: (_) => setState(() => _kind = kind), visualDensity: VisualDensity.compact),
              if (_isChat) ...<Widget>[
                FilterChip(label: Text(t.filterTools), selected: _tools, onSelected: (value) => setState(() => _tools = value), visualDensity: VisualDensity.compact),
                FilterChip(label: Text(t.filterVision), selected: _vision, onSelected: (value) => setState(() => _vision = value), visualDensity: VisualDensity.compact),
              ],
            ],
          ),
        ),
        const Divider(height: 1),
        Flexible(
          child: _loading
              ? const Padding(padding: EdgeInsets.all(40), child: Center(child: SizedBox.square(dimension: 22, child: CircularProgressIndicator(strokeWidth: 2))))
              : _error != null
              ? Padding(padding: const EdgeInsets.all(24), child: InlineError(message: _error!))
              : visible.isEmpty && routes.isEmpty
              ? Padding(padding: const EdgeInsets.all(32), child: Text(t.pickModelNone, textAlign: TextAlign.center, style: TextStyle(color: palette.textSoft)))
              : ListView(
                  shrinkWrap: true,
                  padding: const EdgeInsets.only(bottom: 8),
                  children: <Widget>[
                    if (routes.isNotEmpty) ...<Widget>[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(20, 14, 20, 4),
                        child: Text(t.groupRouting.toUpperCase(), style: sectionEyebrowStyle(palette)),
                      ),
                      for (final route in routes) _choice(palette, id: route.$1, title: route.$2, subtitle: route.$1),
                    ],
                    for (final entry in groups.entries) ...<Widget>[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(20, 14, 20, 4),
                        child: Text(entry.key.toUpperCase(), style: sectionEyebrowStyle(palette)),
                      ),
                      for (final model in entry.value) _row(t, palette, model),
                    ],
                  ],
                ),
        ),
        if (widget.multi) ...<Widget>[
          const Divider(height: 1),
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 10, 16, 12),
            child: Row(
              children: <Widget>[
                Expanded(child: Text(t.pickModelsCount('${_chosen.length}'), style: TextStyle(color: palette.textSoft, fontSize: 13))),
                TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
                const SizedBox(width: 6),
                FilledButton(onPressed: () => Navigator.of(context).pop(_chosen.toList()), child: Text(t.done)),
              ],
            ),
          ),
        ],
      ],
    );
  }

  Widget _row(AppL10n t, NeoRouterPalette palette, CatalogModel model) {
    final facts = <String>[
      modelPriceLabel(t, model),
      if (model.contextWindow != null) t.pickModelContext(formatCount(model.contextWindow!)),
    ];
    return _choice(
      palette,
      id: model.id,
      title: model.name,
      subtitle: facts.join(' · '),
      subtitleColor: model.isFree ? palette.accent : null,
      trailing: <Widget>[
        if (model.tools == true) Padding(padding: const EdgeInsets.only(left: 6), child: Tooltip(message: t.filterTools, child: Icon(Icons.build_outlined, size: 15, color: palette.textMuted))),
        if (model.vision == true) Padding(padding: const EdgeInsets.only(left: 6), child: Tooltip(message: t.filterVision, child: Icon(Icons.image_outlined, size: 15, color: palette.textMuted))),
        if (model.reasoning == true) Padding(padding: const EdgeInsets.only(left: 6), child: Tooltip(message: t.pickModelReasoning, child: Icon(Icons.psychology_outlined, size: 16, color: palette.textMuted))),
      ],
    );
  }

  Widget _choice(NeoRouterPalette palette, {required String id, required String title, required String subtitle, Color? subtitleColor, List<Widget> trailing = const <Widget>[]}) {
    final chosen = _chosen.contains(id);
    return InkWell(
      onTap: () => _tap(id),
      child: Container(
        color: chosen ? palette.accentSoft : null,
        padding: const EdgeInsets.fromLTRB(20, 9, 16, 9),
        child: Row(
          children: <Widget>[
            if (widget.multi) ...<Widget>[
              Icon(chosen ? Icons.check_box_rounded : Icons.check_box_outline_blank_rounded, size: 20, color: chosen ? palette.accent : palette.textMuted),
              const SizedBox(width: 12),
            ],
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(title, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text), overflow: TextOverflow.ellipsis),
                  const SizedBox(height: 2),
                  Text(subtitle, style: TextStyle(fontSize: 12.5, color: subtitleColor ?? palette.textMuted)),
                ],
              ),
            ),
            ...trailing,
            if (!widget.multi && chosen) Padding(padding: const EdgeInsets.only(left: 8), child: Icon(Icons.check_rounded, size: 18, color: palette.accent)),
          ],
        ),
      ),
    );
  }
}
