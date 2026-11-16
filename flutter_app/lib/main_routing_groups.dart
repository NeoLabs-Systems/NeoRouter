import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'src/models.dart';

class RoutingGroups extends StatefulWidget {
  const RoutingGroups({super.key, required this.controller});
  final NeoRouterController controller;

  @override
  State<RoutingGroups> createState() => _RoutingGroupsState();
}

class _RoutingGroupsState extends State<RoutingGroups> {
  List<Map<String, dynamic>> _groups = [];
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final groups = await widget.controller.api.routingGroups();
      if (mounted) {
        setState(() {
          _groups = groups;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    }
  }

  Future<void> _edit([Map<String, dynamic>? group]) async {
    await showDialog<void>(
      context: context,
      builder: (_) => _GroupEditor(
        controller: widget.controller,
        group: group,
        groups: _groups,
      ),
    );
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SettingsHeading(title: t.routingGroups),
        Text(t.routingGroupsHint),
        if (_error != null) InlineError(message: _error!),
        const SizedBox(height: 12),
        SettingsCardList(
          children: [
            for (final group in _groups)
              ListTile(
                title: Text('pool:${group['name']}'),
                subtitle: Text(
                  [
                    if (group['profile'] != null) '${group['profile']}',
                    if (group['fallback'] != null) '→ ${group['fallback']}',
                    for (final plan in (group['plans'] as List? ?? []))
                      '${plan['name']}: ${plan['usedShare'] == null ? t.planUsageUnknown : '${((plan['usedShare'] as num) * 100).round()} % ${t.planUsage}'}',
                  ].join(' · '),
                ),
                onTap: widget.controller.can('routing.edit')
                    ? () => _edit(group)
                    : null,
              ),
          ],
        ),
        if (widget.controller.can('routing.edit'))
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              onPressed: _edit,
              icon: const Icon(Icons.add),
              label: Text(t.createRoutingGroup),
            ),
          ),
      ],
    );
  }
}

class _GroupEditor extends StatefulWidget {
  const _GroupEditor({
    required this.controller,
    required this.groups,
    this.group,
  });
  final NeoRouterController controller;
  final List<Map<String, dynamic>> groups;
  final Map<String, dynamic>? group;

  @override
  State<_GroupEditor> createState() => _GroupEditorState();
}

class _GroupEditorState extends State<_GroupEditor> {
  late final _name = TextEditingController(
    text: widget.group?['name'] as String? ?? '',
  );
  late final _models = TextEditingController(
    text: (widget.group?['models'] as List? ?? []).join('\n'),
  );
  late final _minimum = TextEditingController(
    text: widget.group?['minIntelligence']?.toString() ?? '',
  );
  late final Set<String> _selected = Set<String>.from(
    widget.group?['accounts'] as List? ?? [],
  );
  late String _profile = widget.group?['profile'] as String? ?? '';
  late String _fallback = widget.group?['fallback'] as String? ?? '';
  List<ProviderAccount> _accounts = [];
  List<RoutingProfile> _profiles = [];
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final accounts = await widget.controller.api.accounts();
      final profiles = await widget.controller.api.profiles();
      if (mounted) {
        setState(() {
          _accounts = accounts;
          _profiles = profiles;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    }
  }

  @override
  void dispose() {
    _name.dispose();
    _models.dispose();
    _minimum.dispose();
    super.dispose();
  }

  Future<void> _save({bool remove = false}) async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      if (remove) {
        await widget.controller.api.deleteRoutingGroup(
          widget.group!['id'] as String,
        );
      } else {
        await widget.controller.api.saveRoutingGroup({
          'name': _name.text.trim(),
          'accounts': _selected.toList(),
          'models': _models.text
              .split('\n')
              .map((line) => line.trim())
              .where((line) => line.isNotEmpty)
              .toList(),
          'profile': _profile.isEmpty ? null : _profile,
          'fallback': _fallback.isEmpty ? null : _fallback,
          'minIntelligence': _minimum.text.trim().isEmpty
              ? null
              : double.parse(_minimum.text.trim()),
        }, id: widget.group?['id'] as String?);
      }
      if (mounted) Navigator.of(context).pop();
    } catch (error) {
      if (mounted) {
        setState(() {
          _error = '$error';
          _saving = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AlertDialog(
      title: Text(t.routingGroups),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (_error != null) InlineError(message: _error!),
              TextField(
                controller: _name,
                enabled: widget.group == null,
                decoration: InputDecoration(labelText: t.routingGroupName),
              ),
              const SizedBox(height: 12),
              Text(t.routingGroupAccounts),
              for (final account in _accounts)
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  title: Text(account.label),
                  value: _selected.contains(account.id),
                  onChanged: (value) => setState(() {
                    if (value == true) {
                      _selected.add(account.id);
                    } else {
                      _selected.remove(account.id);
                    }
                  }),
                ),
              TextField(
                controller: _models,
                minLines: 2,
                maxLines: 5,
                decoration: InputDecoration(labelText: t.routingGroupModels),
              ),
              TextField(
                controller: _minimum,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: InputDecoration(labelText: t.routingGroupMinimum),
              ),
              if (_profiles.isNotEmpty)
                DropdownButtonFormField<String>(
                  initialValue:
                      _profiles.any((profile) => profile.id == _profile)
                      ? _profile
                      : '',
                  decoration: InputDecoration(labelText: t.defaultProfile),
                  items: [
                    DropdownMenuItem(
                      value: '',
                      child: Text(t.routingGroupNone),
                    ),
                    for (final profile in _profiles)
                      DropdownMenuItem(
                        value: profile.id,
                        child: Text(profile.name),
                      ),
                  ],
                  onChanged: (value) => setState(() => _profile = value ?? ''),
                ),
              DropdownButtonFormField<String>(
                initialValue: _fallback,
                decoration: InputDecoration(labelText: t.routingGroupFallback),
                items: [
                  DropdownMenuItem(value: '', child: Text(t.routingGroupNone)),
                  for (final group in widget.groups.where(
                    (group) => group['id'] != widget.group?['id'],
                  ))
                    DropdownMenuItem(
                      value: group['name'] as String,
                      child: Text('${group['name']}'),
                    ),
                ],
                onChanged: (value) => setState(() => _fallback = value ?? ''),
              ),
            ],
          ),
        ),
      ),
      actions: [
        if (widget.group != null)
          TextButton(
            onPressed: _saving ? null : () => _save(remove: true),
            child: Text(t.delete),
          ),
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(),
          child: Text(t.cancel),
        ),
        FilledButton(onPressed: _saving ? null : _save, child: Text(t.save)),
      ],
    );
  }
}
