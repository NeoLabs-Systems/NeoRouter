import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_selection.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'routing_group_setup.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// Routing groups: one kind of work (an agent's helpers, heavy jobs) goes to
/// the plans the person chooses. Apps pick a group with pool:name.
class RoutingGroupsScreen extends StatefulWidget {
  const RoutingGroupsScreen({
    super.key,
    required this.controller,
    this.appSetup,
  });
  final NeoRouterController controller;
  final Widget Function(String model)? appSetup;

  @override
  State<RoutingGroupsScreen> createState() => _RoutingGroupsScreenState();
}

class _RoutingGroupsScreenState extends State<RoutingGroupsScreen> {
  List<Map<String, dynamic>> _groups = [];
  Map<String, String> _profileNames = {};
  Map<String, String> _accountNames = {};
  bool _loading = true;
  String? _error;

  bool get _canEdit => widget.controller.can('routing.edit');

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final groups = await widget.controller.api.routingGroups();
      final profiles = await widget.controller.api.profiles();
      final accounts = await widget.controller.api.accounts();
      if (!mounted) return;
      setState(() {
        _groups = groups;
        _profileNames = {
          for (final profile in profiles) profile.id: profile.name,
        };
        _accountNames = {
          for (final account in accounts) account.id: account.label,
        };
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = '$error';
        });
      }
    }
  }

  Future<void> _edit([Map<String, dynamic>? group]) async {
    final changed = await showDialog<String>(
      context: context,
      builder: (_) => _GroupEditor(
        controller: widget.controller,
        group: group,
        groups: _groups,
      ),
    );
    if (!mounted) return;
    await _load();
    if (group == null && changed != null && changed.isNotEmpty && mounted) {
      _showSetup(
        _groups.firstWhere(
          (item) => item['name'] == changed,
          orElse: () => {'name': changed},
        ),
      );
    }
  }

  void _showSetup(Map<String, dynamic> group) {
    showDialog<void>(
      context: context,
      builder: (_) => RoutingGroupSetup(
        controller: widget.controller,
        name: group['name'] as String,
        subagent: group['subagentGroup'] as String?,
        appSetupBuilder:
            widget.appSetup == null || !widget.controller.can('keys.view')
            ? null
            : (_) => widget.appSetup!('pool:${group['name']}'),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.routingGroups,
        description: t.routingGroupsDescription,
        trailing: _canEdit && _groups.isNotEmpty
            ? FilledButton.icon(
                onPressed: _edit,
                icon: const Icon(Icons.add, size: 18),
                label: Text(t.createRoutingGroup),
              )
            : null,
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _groups.isEmpty
              ? EmptyState(
                  icon: Icons.workspaces_outline,
                  title: t.routingGroupsEmptyTitle,
                  message: t.routingGroupsEmpty,
                  action: _canEdit
                      ? FilledButton.icon(
                          onPressed: _edit,
                          icon: const Icon(Icons.add, size: 18),
                          label: Text(t.createRoutingGroup),
                        )
                      : null,
                )
              : LayoutBuilder(
                  builder: (context, constraints) {
                    final columns = constraints.maxWidth >= 900 ? 2 : 1;
                    final width =
                        (constraints.maxWidth - (columns - 1) * AppSpacing.md) /
                        columns;
                    return Wrap(
                      spacing: AppSpacing.md,
                      runSpacing: AppSpacing.md,
                      children: <Widget>[
                        for (final group in _groups)
                          SizedBox(
                            width: width,
                            child: _GroupCard(
                              group: group,
                              onSetup: () => _showSetup(group),
                              profileName: _profileNames[group['profile']],
                              accountNames: _accountNames,
                              onTap: _canEdit ? () => _edit(group) : null,
                            ),
                          ),
                      ],
                    );
                  },
                ),
        ),
      ],
    );
  }
}

class _GroupCard extends StatelessWidget {
  const _GroupCard({
    required this.group,
    required this.profileName,
    required this.accountNames,
    required this.onSetup,
    this.onTap,
  });
  final Map<String, dynamic> group;
  final String? profileName;
  final Map<String, String> accountNames;
  final VoidCallback? onTap;
  final VoidCallback onSetup;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final name = '${group['name']}';
    final accounts = [
      for (final id in (group['accounts'] as List? ?? []))
        accountNames[id] ?? '$id',
    ];
    final models = [
      for (final pattern in (group['models'] as List? ?? [])) '$pattern',
    ];
    final members = [...accounts, ...models];
    final plans = (group['plans'] as List? ?? []).cast<Map>();
    final minimum = group['minIntelligence'] as num?;
    final details = <String>[
      if (accountNames[group['preferredAccount']] case final String preferred)
        t.routingGroupPrefers(preferred),
      if (group['subagentGroup'] case final String subagent)
        t.routingGroupUsesSubagent(subagent),
      if (profileName != null) t.routingGroupUsesStyle(profileName!),
      if (minimum != null) t.routingGroupAtLeast('${(minimum * 100).round()}'),
      if (group['fallback'] != null)
        t.routingGroupFallsBackTo('pool:${group['fallback']}'),
    ];
    const shown = 5;
    return AppCard(
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  name,
                  style: displayTitleStyle(palette, size: 18),
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              TextButton(
                onPressed: onSetup,
                child: Text(t.routingGroupUseInApp),
              ),
              if (onTap != null)
                Icon(Icons.chevron_right, color: palette.textSoft),
            ],
          ),
          if (details.isNotEmpty) ...<Widget>[
            const SizedBox(height: 4),
            Text(
              details.join(' · '),
              style: TextStyle(color: palette.textSoft, fontSize: 13),
            ),
          ],
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              for (final member in members.take(shown)) MetaPill(label: member),
              if (members.length > shown)
                MetaPill(label: '+${members.length - shown}'),
            ],
          ),
          if (plans.isNotEmpty) ...<Widget>[
            const SizedBox(height: AppSpacing.md),
            for (final plan in plans) ...<Widget>[
              Row(
                children: <Widget>[
                  Expanded(
                    child: Text(
                      '${plan['name']}',
                      style: const TextStyle(fontSize: 13),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  Text(
                    plan['usedShare'] is num
                        ? t.routingGroupPlanUsed(
                            '${((plan['usedShare'] as num) * 100).round()}',
                          )
                        : t.planUsageUnknown,
                    style: TextStyle(color: palette.textSoft, fontSize: 12),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              UsageBar(
                fraction: plan['usedShare'] is num
                    ? (plan['usedShare'] as num).toDouble()
                    : 0,
              ),
              const SizedBox(height: AppSpacing.xs),
            ],
          ],
        ],
      ),
    );
  }
}

class _GroupEditor extends StatefulWidget {
  const _GroupEditor({
    required this.controller,
    required this.groups,
    this.group,
    this.creatingSubagent = false,
  });
  final NeoRouterController controller;
  final List<Map<String, dynamic>> groups;
  final Map<String, dynamic>? group;
  final bool creatingSubagent;

  @override
  State<_GroupEditor> createState() => _GroupEditorState();
}

class _GroupEditorState extends State<_GroupEditor> {
  static final _namePattern = RegExp(r'^[a-z0-9][a-z0-9_-]{0,79}$');

  late final _name = TextEditingController(
    text: widget.group?['name'] as String? ?? '',
  );
  final _search = TextEditingController();
  late final Set<String> _selected = Set<String>.from(
    widget.group?['accounts'] as List? ?? [],
  );
  late final List<String> _models = [
    for (final item in (widget.group?['models'] as List? ?? [])) '$item',
  ];
  late double? _minimum = (widget.group?['minIntelligence'] as num?)
      ?.toDouble();
  late String _profile = widget.group?['profile'] as String? ?? '';
  late String _fallback = widget.group?['fallback'] as String? ?? '';
  late String _preferred = widget.group?['preferredAccount'] as String? ?? '';
  late String _subagent = widget.group?['subagentGroup'] as String? ?? '';
  late bool _allowOutside =
      widget.group != null && widget.group!['allowOutside'] != false;
  late bool _allowMetered =
      widget.group != null && widget.group!['allowMetered'] != false;
  late List<Map<String, dynamic>> _groups = widget.groups;
  bool _loadingGroups = false;
  List<ProviderAccount> _accounts = [];
  List<RoutingProfile> _profiles = [];
  bool _loading = true;
  bool _saving = false;
  String? _error;

  bool get _creating => widget.group == null;
  String get _aliasName =>
      _name.text.trim().toLowerCase().replaceAll(RegExp(r'\s+'), '-');

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final accounts = await widget.controller.api.accounts();
      final profiles = await widget.controller.api.profiles();
      if (!mounted) return;
      setState(() {
        _accounts = accounts;
        _profiles = profiles;
        _loading = false;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          _error = '$error';
        });
      }
    }
  }

  @override
  void dispose() {
    _name.dispose();
    _search.dispose();
    super.dispose();
  }

  String? _validate(AppL10n t) {
    if (_creating && !_namePattern.hasMatch(_aliasName)) {
      return t.routingGroupNameInvalid;
    }
    if (_selected.isEmpty && _models.isEmpty) {
      return t.routingGroupMembersMissing;
    }
    return null;
  }



  Future<void> _save() async {
    final t = AppL10n.of(context);
    final invalid = _validate(t);
    if (invalid != null) {
      setState(() => _error = invalid);
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.controller.api.saveRoutingGroup({
        'name': _creating ? _aliasName : widget.group!['name'],
        'accounts': _selected.toList(),
        'models': _models,
        'profile': _profile.isEmpty ? null : _profile,
        'fallback': _fallback.isEmpty ? null : _fallback,
        'minIntelligence': _minimum,
        'preferredAccount': _preferred.isEmpty ? null : _preferred,
        'subagentGroup': _subagent.isEmpty ? null : _subagent,
        'allowOutside': _allowOutside,
        'allowMetered': _allowMetered,
      }, id: widget.group?['id'] as String?);
      if (mounted) Navigator.of(context).pop(_aliasName);
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = error is ApiException ? error.message : '$error';
          _saving = false;
        });
      }
    }
  }

  Future<void> _delete() async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(
      context,
      title: t.routingGroupDeleteTitle,
      message: t.routingGroupDeleteMessage('${widget.group!['name']}'),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
    );
    if (!confirmed || !mounted) return;
    setState(() => _saving = true);
    try {
      await widget.controller.api.deleteRoutingGroup(
        widget.group!['id'] as String,
      );
      if (mounted) Navigator.of(context).pop('');
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = error is ApiException ? error.message : '$error';
          _saving = false;
        });
      }
    }
  }

  Widget _section(String title, String hint, List<Widget> children) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(
            title,
            style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15),
          ),
          const SizedBox(height: 2),
          Text(
            hint,
            style: TextStyle(
              color: palette.textSoft,
              fontSize: 13,
              height: 1.4,
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          ...children,
        ],
      ),
    );
  }

  Widget _accountPicker(AppL10n t) {
    final palette = neoRouterPaletteOf(context);
    final query = _search.text.trim().toLowerCase();
    final byProvider = <String, List<ProviderAccount>>{};
    for (final account in _accounts) {
      if (query.isNotEmpty &&
          !'${account.label} ${account.providerName}'.toLowerCase().contains(
            query,
          )) {
        continue;
      }
      byProvider.putIfAbsent(account.providerName, () => []).add(account);
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (_accounts.length > 6) ...<Widget>[
          TextField(
            controller: _search,
            onChanged: (_) => setState(() {}),
            decoration: InputDecoration(
              isDense: true,
              prefixIcon: const Icon(Icons.search, size: 18),
              hintText: t.routingGroupSearchAccounts,
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
        ],
        if (byProvider.isEmpty)
          Text(
            t.routingGroupNoAccounts,
            style: TextStyle(color: palette.textSoft, fontSize: 13),
          ),
        for (final entry in [
          ...byProvider.entries.where((entry) => entry.value.length > 1),
          if (byProvider.values.any((list) => list.length == 1))
            MapEntry<String, List<ProviderAccount>>(
              byProvider.values.any((list) => list.length > 1)
                  ? t.routingGroupOtherAccounts
                  : '',
              [
                for (final list in byProvider.values.where(
                  (list) => list.length == 1,
                ))
                  ...list,
              ],
            ),
        ]) ...<Widget>[
          if (entry.key.isNotEmpty) ...<Widget>[
            Text(
              entry.key,
              style: TextStyle(
                color: palette.textSoft,
                fontSize: 12,
                fontWeight: FontWeight.w600,
              ),
            ),
            const SizedBox(height: 6),
          ],
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              for (final account in entry.value)
                FilterChip(
                  label: Text(
                    account.label == account.providerName ||
                            entry.key == account.providerName
                        ? account.label
                        : '${account.label} · ${account.providerName}',
                  ),
                  selected: _selected.contains(account.id),
                  showCheckmark: true,
                  onSelected: (on) => setState(() {
                    if (on) {
                      _selected.add(account.id);
                    } else {
                      _selected.remove(account.id);
                      if (_preferred == account.id) _preferred = '';
                    }
                  }),
                ),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
        ],
      ],
    );
  }

  Widget _modelList(AppL10n t) => ModelSelectionField(
    controller: widget.controller,
    title: t.routingGroupPickModels,
    emptyHint: '',
    value: _models,
    onChanged: (value) => setState(() => _models
      ..clear()
      ..addAll(value)),
  );

  Widget _withOptionHelp({
    required String title,
    required String explanation,
    required Widget child,
  }) {
    final t = AppL10n.of(context);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(child: child),
        IconButton(
          tooltip: t.routingGroupOptionHelp(title),
          icon: Icon(
            Icons.info_outline,
            size: 18,
            color: neoRouterPaletteOf(context).textMuted,
          ),
          onPressed: () => showDialog<void>(
            context: context,
            builder: (context) => AlertDialog(
              title: Text(title),
              scrollable: true,
              content: SizedBox(width: 360, child: Text(explanation)),
              actions: [
                TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: Text(t.close),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _preferredPicker(AppL10n t) {
    final accounts = _accounts
        .where((account) => _selected.contains(account.id))
        .toList();
    if (accounts.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.lg),
      child: _withOptionHelp(
        title: t.routingGroupPrefer,
        explanation: t.routingGroupsHelpSwitch,
        child: DropdownButtonFormField<String>(
          key: ValueKey(_preferred),
          initialValue: accounts.any((account) => account.id == _preferred)
              ? _preferred
              : '',
          isExpanded: true,
          decoration: InputDecoration(
            labelText: t.routingGroupPrefer,
            helperText: t.routingGroupPreferHint,
          ),
          items: [
            DropdownMenuItem(value: '', child: Text(t.routingGroupAutomatic)),
            for (final account in accounts)
              DropdownMenuItem(
                value: account.id,
                child: Text(
                  account.label == account.providerName
                      ? account.label
                      : '${account.label} · ${account.providerName}',
                  overflow: TextOverflow.ellipsis,
                ),
              ),
          ],
          onChanged: (value) => setState(() => _preferred = value ?? ''),
        ),
      ),
    );
  }

  Future<void> _createSubagent() async {
    final name = await showDialog<String>(
      context: context,
      builder: (_) => _GroupEditor(
        controller: widget.controller,
        groups: _groups,
        creatingSubagent: true,
      ),
    );
    if (name == null || name.isEmpty || !mounted) return;
    setState(() => _loadingGroups = true);
    try {
      final groups = await widget.controller.api.routingGroups();
      if (!mounted) return;
      setState(() {
        _groups = groups;
        _subagent = name;
      });
    } catch (error) {
      if (mounted) {
        setState(
          () => _error = error is ApiException ? error.message : '$error',
        );
      }
    } finally {
      if (mounted) setState(() => _loadingGroups = false);
    }
  }

  Widget _subagentPicker(AppL10n t, List<Map<String, dynamic>> groups) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _withOptionHelp(
          title: t.routingGroupSubagents,
          explanation: t.routingGroupsHelpSubagents,
          child: DropdownButtonFormField<String>(
            key: ValueKey(_subagent),
            initialValue: _subagent,
            isExpanded: true,
            decoration: InputDecoration(labelText: t.routingGroupSubagents),
            items: [
              DropdownMenuItem(value: '', child: Text(t.routingGroupSameGroup)),
              for (final group in groups)
                DropdownMenuItem(
                  value: group['name'] as String,
                  child: Text('${group['name']}'),
                ),
            ],
            onChanged: _loadingGroups
                ? null
                : (value) => setState(() => _subagent = value ?? ''),
          ),
        ),
        if (_subagent.isNotEmpty)
          Padding(
            padding: const EdgeInsets.only(top: AppSpacing.xs),
            child: Text(t.routingGroupSubagentHint),
          ),
        TextButton.icon(
          onPressed: _loadingGroups ? null : _createSubagent,
          icon: const Icon(Icons.add, size: 18),
          label: Text(t.routingGroupCreateSubagent),
        ),
      ],
    );
  }

  Widget _options(AppL10n t) {
    final palette = neoRouterPaletteOf(context);
    final fallbacks = _groups
        .where((group) => group['id'] != widget.group?['id'])
        .toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(t.routingGroupMinimumTitle),
          subtitle: Text(
            _minimum == null
                ? t.routingGroupMinimumOff
                : t.routingGroupAtLeast('${(_minimum! * 100).round()}'),
          ),
          value: _minimum != null,
          onChanged: (on) => setState(() => _minimum = on ? 0.5 : null),
        ),
        if (_minimum != null)
          Slider(
            value: _minimum!,
            divisions: 20,
            label: '${(_minimum! * 100).round()} %',
            activeColor: palette.accent,
            onChanged: (value) => setState(() => _minimum = value),
          ),
        const SizedBox(height: AppSpacing.md),
        DropdownButtonFormField<String>(
          initialValue: _profiles.any((profile) => profile.id == _profile)
              ? _profile
              : '',
          isExpanded: true,
          decoration: InputDecoration(
            labelText: t.routingGroupStyle,
            helperText: t.routingGroupStyleHint,
            helperMaxLines: 2,
          ),
          items: [
            DropdownMenuItem(value: '', child: Text(t.routingGroupStyleKey)),
            for (final profile in _profiles)
              DropdownMenuItem(value: profile.id, child: Text(profile.name)),
          ],
          onChanged: (value) => setState(() => _profile = value ?? ''),
        ),
        const SizedBox(height: AppSpacing.lg),
        _withOptionHelp(
          title: t.routingGroupFallback,
          explanation: t.routingGroupFallbackHelp,
          child: DropdownButtonFormField<String>(
            initialValue: fallbacks.any((group) => group['name'] == _fallback)
                ? _fallback
                : '',
            isExpanded: true,
            decoration: InputDecoration(
              labelText: t.routingGroupFallback,
              helperText: t.routingGroupFallbackHint,
              helperMaxLines: 2,
            ),
            items: [
              DropdownMenuItem(
                value: '',
                child: Text(t.routingGroupNoFallback),
              ),
              for (final group in fallbacks)
                DropdownMenuItem(
                  value: group['name'] as String,
                  child: Text('${group['name']}'),
                ),
            ],
            onChanged: (value) => setState(() => _fallback = value ?? ''),
          ),
        ),
        const SizedBox(height: AppSpacing.md),
        _withOptionHelp(
          title: t.routingGroupAllowOutside,
          explanation: t.routingGroupOutsideHelp,
          child: SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: Text(t.routingGroupAllowOutside),
            value: _allowOutside,
            onChanged: (value) => setState(() => _allowOutside = value),
          ),
        ),
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(t.routingGroupAllowMetered),
          subtitle: Text(t.routingGroupMeteredHint),
          value: _allowMetered,
          onChanged: (value) => setState(() => _allowMetered = value),
        ),
        if (!widget.creatingSubagent) ...[
          const SizedBox(height: AppSpacing.md),
          _subagentPicker(t, fallbacks),
        ],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final mobile = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    return Dialog(
      insetPadding: mobile
          ? const EdgeInsets.all(12)
          : const EdgeInsets.all(40),
      clipBehavior: Clip.antiAlias,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640, maxHeight: 760),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 20, 12, 8),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: Text(
                      _creating ? t.createRoutingGroup : _name.text,
                      style: displayTitleStyle(palette, size: 20),
                    ),
                  ),
                  IconButton(
                    icon: const Icon(Icons.close),
                    tooltip: t.cancel,
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                ],
              ),
            ),
            Flexible(
              child: _loading
                  ? const Padding(
                      padding: EdgeInsets.all(48),
                      child: Center(child: CircularProgressIndicator()),
                    )
                  : SingleChildScrollView(
                      padding: const EdgeInsets.fromLTRB(24, 8, 24, 8),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: <Widget>[
                          if (_creating)
                            Padding(
                              padding: const EdgeInsets.only(
                                bottom: AppSpacing.lg,
                              ),
                              child: TextField(
                                controller: _name,
                                autofocus: true,
                                decoration: InputDecoration(
                                  labelText: t.routingGroupName,
                                ),
                              ),
                            ),
                          _section(
                            t.routingGroupAccounts,
                            t.routingGroupAccountsHint,
                            [_accountPicker(t)],
                          ),
                          _preferredPicker(t),
                          ExpansionTile(
                            tilePadding: EdgeInsets.zero,
                            childrenPadding: const EdgeInsets.only(
                              bottom: AppSpacing.md,
                            ),
                            title: Text(t.routingGroupMore),
                            expandedCrossAxisAlignment:
                                CrossAxisAlignment.stretch,
                            children: [
                              _section(
                                t.routingGroupModelsTitle,
                                t.routingGroupModelsHint,
                                [_modelList(t)],
                              ),
                              _options(t),
                            ],
                          ),
                        ],
                      ),
                    ),
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(24, 0, 24, 8),
                child: InlineError(message: _error!),
              ),
            Divider(height: 1, color: palette.border),
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 24, 16),
              child: Row(
                children: <Widget>[
                  if (!_creating)
                    TextButton.icon(
                      onPressed: _saving ? null : _delete,
                      style: TextButton.styleFrom(
                        foregroundColor: palette.error,
                      ),
                      icon: const Icon(Icons.delete_outline, size: 18),
                      label: Text(t.delete),
                    ),
                  const Spacer(),
                  TextButton(
                    onPressed: _saving
                        ? null
                        : () => Navigator.of(context).pop(),
                    child: Text(t.cancel),
                  ),
                  const SizedBox(width: AppSpacing.xs),
                  FilledButton(
                    onPressed: _saving || _loading || _loadingGroups
                        ? null
                        : _save,
                    child: Text(t.save),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
