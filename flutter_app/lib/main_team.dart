import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// Permission rows grouped by area, in the order the server lists them.
List<(String, List<(String, String)>)> permissionGroups(AppL10n t) => <(String, List<(String, String)>)>[
      (t.navActivity, <(String, String)>[('activity.view', t.permActivityView)]),
      (t.navPlayground, <(String, String)>[('playground.use', t.permPlaygroundUse)]),
      (t.navKeys, <(String, String)>[
        ('keys.view', t.permView),
        ('keys.create', t.permCreate),
        ('keys.edit', t.permEdit),
        ('keys.delete', t.permDelete),
      ]),
      (t.navProviders, <(String, String)>[
        ('providers.view', t.permView),
        ('providers.manage', t.permProvidersManage),
        ('providers.delete', t.permDelete),
        ('models.manage', t.permModelsManage),
      ]),
      (t.navRouting, <(String, String)>[('routing.view', t.permView), ('routing.edit', t.permEdit)]),
      (t.navProcessing, <(String, String)>[('processing.view', t.permView), ('processing.edit', t.permEdit)]),
    ];

String presetLabel(AppL10n t, String preset) => switch (preset) {
      'view' => t.presetView,
      'keys' => t.presetKeys,
      _ => t.presetFull,
    };

/// The view right an edit right depends on, as on the server.
String? _viewFor(String permission) {
  final parts = permission.split('.');
  if (parts.last == 'view' || parts.last == 'use') return null;
  return parts.first == 'models' ? 'providers.view' : '${parts.first}.view';
}

class TeamScreen extends StatefulWidget {
  const TeamScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<TeamScreen> createState() => _TeamScreenState();
}

class _TeamScreenState extends State<TeamScreen> {
  TeamSummary? _team;
  bool _loading = true;
  String? _error;

  NeoRouterController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load().then((_) {
      final invite = controller.pendingInvite;
      if (invite != null && mounted) {
        controller.consumeInvite();
        _redeem(invite);
      }
    });
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final team = await controller.api.team();
      setState(() => _team = team);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _apply(Future<TeamSummary> Function() action) async {
    try {
      final team = await action();
      if (!mounted) return;
      setState(() => _team = team);
      await controller.refreshTeam();
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  Future<void> _createInvite() async {
    final team = _team;
    if (team == null) return;
    final link = await showDialog<String>(context: context, builder: (_) => _CreateInviteDialog(controller: controller, team: team));
    if (link == null || !mounted) return;
    await _load();
    if (mounted) await _showLink(link);
  }

  Future<void> _showLink(String link) async {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.inviteLinkTitle),
        content: SizedBox(
          width: 480,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(t.inviteLinkBody, style: TextStyle(color: palette.textSoft, height: 1.45)),
              const SizedBox(height: AppSpacing.md),
              Container(
                padding: const EdgeInsets.fromLTRB(14, 12, 4, 12),
                decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
                child: Row(
                  children: <Widget>[
                    Expanded(child: SelectableText(link, style: GoogleFonts.geistMono(fontSize: 12.5, color: palette.text))),
                    IconButton(
                      tooltip: t.copy,
                      icon: const Icon(Icons.copy_rounded, size: 17),
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: link));
                        showSnack(context, t.copied);
                      },
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.keySecretDone))],
      ),
    );
  }

  Future<void> _redeem([String? link]) async {
    final joined = await showDialog<TeamSummary>(context: context, builder: (_) => _RedeemDialog(controller: controller, initialLink: link));
    if (joined == null || !mounted) return;
    setState(() => _team = joined);
    await controller.refreshTeam();
  }

  Future<void> _editMember(TeamMember member) async {
    final team = _team;
    if (team == null) return;
    final next = await showDialog<List<String>>(
      context: context,
      builder: (_) => _PermissionsDialog(title: member.user.displayName, team: team, initial: member.permissions.toSet()),
    );
    if (next != null) await _apply(() => controller.api.setMemberPermissions(member.user.id, next));
  }

  Future<void> _release(TeamMember member) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.removeMember, message: t.removeMemberConfirm(member.user.displayName), confirmLabel: t.removeMember, cancelLabel: t.cancel);
    if (confirmed) await _apply(() => controller.api.releaseMember(member.user.id));
  }

  Future<void> _leave(TeamUser owner) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.leaveTeam, message: t.leaveTeamConfirm(owner.displayName), confirmLabel: t.leaveTeam, cancelLabel: t.cancel);
    if (confirmed) await _apply(controller.api.leaveTeam);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final team = _team;
    return PageScaffold(
      header: ScreenHeader(
        title: t.teamTitle,
        description: t.teamDescription,
        trailing: FilledButton.icon(onPressed: team == null ? null : _createInvite, icon: const Icon(Icons.person_add_alt_1_outlined, size: 18), label: Text(t.inviteSomeone)),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: team == null
              ? const SizedBox()
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    _memberOf(t, palette, team),
                    SettingsGroup(
                      title: t.teamMembersTitle,
                      description: t.teamMembersHint,
                      children: team.members.isEmpty
                          ? <Widget>[
                              EmptyState(
                                icon: Icons.groups_2_outlined,
                                title: t.noMembersTitle,
                                message: t.noMembersMessage,
                                action: OutlinedButton.icon(onPressed: _createInvite, icon: const Icon(Icons.person_add_alt_1_outlined, size: 18), label: Text(t.inviteSomeone)),
                              ),
                            ]
                          : <Widget>[for (final member in team.members) _memberRow(t, palette, team, member)],
                    ),
                    if (team.invites.any((invite) => invite.active))
                      SettingsGroup(
                        title: t.inviteLinksTitle,
                        description: t.inviteLinksHint,
                        children: <Widget>[for (final invite in team.invites.where((item) => item.active)) _inviteRow(t, palette, invite)],
                      ),
                  ],
                ),
        ),
      ],
    );
  }

  Widget _memberOf(AppL10n t, NeoRouterPalette palette, TeamSummary team) {
    final owner = team.owner;
    if (owner == null) {
      return SettingsGroup(
        title: t.joinTeamTitle,
        children: <Widget>[
          SettingsRow(
            label: t.joinTeamLabel,
            description: t.joinTeamHint,
            control: OutlinedButton.icon(onPressed: _redeem, icon: const Icon(Icons.link_rounded, size: 18), label: Text(t.useInviteLink)),
          ),
        ],
      );
    }
    final granted = team.grantedToMe;
    return SettingsGroup(
      title: t.memberOfTitle(owner.displayName),
      description: t.memberOfHint,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.all(20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(t.youMay, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
              const SizedBox(height: 8),
              _PermissionSummary(granted: granted),
              const SizedBox(height: 16),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: <Widget>[
                  FilledButton.icon(
                    onPressed: () => controller.switchSpace(owner.id),
                    icon: const Icon(Icons.swap_horiz_rounded, size: 18),
                    label: Text(t.openSpaceOf(owner.displayName)),
                  ),
                  TextButton(onPressed: () => _leave(owner), child: Text(t.leaveTeam, style: TextStyle(color: palette.error))),
                ],
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _memberRow(AppL10n t, NeoRouterPalette palette, TeamSummary team, TeamMember member) {
    final initial = member.user.displayName.trim().isEmpty ? '?' : member.user.displayName.trim()[0].toUpperCase();
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 14, 8, 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Container(
            width: 32,
            height: 32,
            alignment: Alignment.center,
            decoration: BoxDecoration(shape: BoxShape.circle, color: palette.surfaceRaised, border: Border.all(color: palette.borderStrong)),
            child: Text(initial, style: TextStyle(color: palette.text, fontWeight: FontWeight.w700, fontSize: 12)),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(member.user.displayName, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text)),
                const SizedBox(height: 2),
                Text(member.user.username, style: TextStyle(color: palette.textMuted, fontSize: 13)),
                const SizedBox(height: 10),
                _PermissionSummary(granted: member.permissions),
              ],
            ),
          ),
          PopupMenuButton<String>(
            tooltip: t.moreActions,
            icon: const Icon(Icons.more_horiz_rounded, size: 20),
            onSelected: (value) => value == 'edit' ? _editMember(member) : _release(member),
            itemBuilder: (_) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(value: 'edit', child: Text(t.editPermissions)),
              PopupMenuItem<String>(value: 'remove', child: Text(t.removeMember, style: TextStyle(color: palette.error))),
            ],
          ),
        ],
      ),
    );
  }

  Widget _inviteRow(AppL10n t, NeoRouterPalette palette, TeamInvite invite) {
    final expires = invite.expiresAt == null ? t.inviteNoExpiry : t.inviteExpires(formatTime(DateTime.parse(invite.expiresAt!).toLocal()));
    return ListRow(
      leading: Icon(Icons.link_rounded, size: 18, color: palette.textSoft),
      title: invite.label.isEmpty ? t.inviteUntitled : invite.label,
      subtitle: <String>[
        invite.singleUse ? t.inviteSingleUse : t.inviteReusable('${invite.useCount}'),
        expires,
        t.permissionCount('${invite.permissions.length}'),
      ].join(' · '),
      trailing: TextButton(
        onPressed: () => _apply(() => controller.api.revokeInvite(invite.id)),
        child: Text(t.revoke, style: TextStyle(color: palette.error)),
      ),
    );
  }
}

/// The granted rights as short tags, one per area.
class _PermissionSummary extends StatelessWidget {
  const _PermissionSummary({required this.granted});

  final List<String> granted;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final tags = <String>[
      for (final (area, rows) in permissionGroups(t))
        if (rows.any((row) => granted.contains(row.$1)))
          '$area: ${rows.where((row) => granted.contains(row.$1)).map((row) => row.$2).join(', ')}',
    ];
    if (tags.isEmpty) return MetaPill(label: t.noPermissions);
    return Wrap(spacing: 6, runSpacing: 6, children: <Widget>[for (final tag in tags) MetaPill(label: tag)]);
  }
}

/// Presets plus one checkbox per right. Ticking a change right also ticks
/// its view right; unticking a view right clears what depends on it.
class _PermissionPicker extends StatelessWidget {
  const _PermissionPicker({required this.team, required this.selected, required this.onChanged});

  final TeamSummary team;
  final Set<String> selected;
  final ValueChanged<Set<String>> onChanged;

  void _toggle(String permission, bool on) {
    final next = <String>{...selected};
    if (on) {
      next.add(permission);
      final view = _viewFor(permission);
      if (view != null) next.add(view);
    } else {
      next.remove(permission);
      next.removeWhere((other) => _viewFor(other) == permission);
    }
    onChanged(next);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            for (final entry in team.presets.entries)
              ChoiceChip(
                label: Text(presetLabel(t, entry.key)),
                selected: selected.length == entry.value.length && selected.containsAll(entry.value),
                onSelected: (_) => onChanged(entry.value.toSet()),
              ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        for (final (area, rows) in permissionGroups(t)) ...<Widget>[
          Padding(
            padding: const EdgeInsets.only(top: 8, bottom: 2),
            child: Text(area, style: TextStyle(color: palette.textMuted, fontSize: 12.5, fontWeight: FontWeight.w600)),
          ),
          Wrap(
            spacing: 4,
            children: <Widget>[
              for (final (permission, label) in rows)
                FilterChip(
                  label: Text(label),
                  selected: selected.contains(permission),
                  onSelected: (on) => _toggle(permission, on),
                ),
            ],
          ),
        ],
      ],
    );
  }
}

class _PermissionsDialog extends StatefulWidget {
  const _PermissionsDialog({required this.title, required this.team, required this.initial});

  final String title;
  final TeamSummary team;
  final Set<String> initial;

  @override
  State<_PermissionsDialog> createState() => _PermissionsDialogState();
}

class _PermissionsDialogState extends State<_PermissionsDialog> {
  late Set<String> _selected = widget.initial;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AlertDialog(
      title: Text(t.permissionsFor(widget.title)),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(child: _PermissionPicker(team: widget.team, selected: _selected, onChanged: (next) => setState(() => _selected = next))),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(widget.team.permissions.where(_selected.contains).toList()),
          child: Text(t.save),
        ),
      ],
    );
  }
}

class _CreateInviteDialog extends StatefulWidget {
  const _CreateInviteDialog({required this.controller, required this.team});

  final NeoRouterController controller;
  final TeamSummary team;

  @override
  State<_CreateInviteDialog> createState() => _CreateInviteDialogState();
}

class _CreateInviteDialogState extends State<_CreateInviteDialog> {
  final TextEditingController _label = TextEditingController();
  late Set<String> _selected = (widget.team.presets['keys'] ?? const <String>[]).toSet();
  int? _expiresInHours = 24 * 7;
  bool _singleUse = true;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _label.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final link = await widget.controller.api.createInvite(
        label: _label.text.trim(),
        permissions: widget.team.permissions.where(_selected.contains).toList(),
        expiresInHours: _expiresInHours,
        singleUse: _singleUse,
      );
      if (mounted) Navigator.of(context).pop(link);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AlertDialog(
      title: Text(t.inviteSomeone),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _label, maxLength: 80, decoration: InputDecoration(labelText: t.inviteLabel, hintText: t.inviteLabelHint, isDense: true)),
              const SizedBox(height: AppSpacing.sm),
              DropdownButtonFormField<int?>(
                initialValue: _expiresInHours,
                decoration: InputDecoration(labelText: t.inviteExpiry, isDense: true),
                items: <DropdownMenuItem<int?>>[
                  DropdownMenuItem<int?>(value: 24, child: Text(t.expiry24h)),
                  DropdownMenuItem<int?>(value: 24 * 7, child: Text(t.expiry7d)),
                  DropdownMenuItem<int?>(value: 24 * 30, child: Text(t.expiry30d)),
                  DropdownMenuItem<int?>(value: null, child: Text(t.expiryNever)),
                ],
                onChanged: (value) => setState(() => _expiresInHours = value),
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(t.inviteSingleUseLabel),
                subtitle: Text(t.inviteSingleUseHint),
                value: _singleUse,
                onChanged: (value) => setState(() => _singleUse = value),
              ),
              const SizedBox(height: AppSpacing.sm),
              Text(t.invitePermissionsLabel, style: sectionTitleStyle(neoRouterPaletteOf(context))),
              const SizedBox(height: AppSpacing.sm),
              _PermissionPicker(team: widget.team, selected: _selected, onChanged: (next) => setState(() => _selected = next)),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _saving ? null : _create, child: Text(t.createInviteLink)),
      ],
    );
  }
}

/// Paste a link, see whose team it is and what it allows, then join.
class _RedeemDialog extends StatefulWidget {
  const _RedeemDialog({required this.controller, this.initialLink});

  final NeoRouterController controller;
  final String? initialLink;

  @override
  State<_RedeemDialog> createState() => _RedeemDialogState();
}

class _RedeemDialogState extends State<_RedeemDialog> {
  late final TextEditingController _link = TextEditingController(text: widget.initialLink ?? '');
  Map<String, dynamic>? _preview;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (_link.text.isNotEmpty) WidgetsBinding.instance.addPostFrameCallback((_) => _check());
  }

  @override
  void dispose() {
    _link.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _check() => _run(() async {
        final preview = await widget.controller.api.previewInvite(_link.text.trim());
        setState(() => _preview = preview);
      });

  Future<void> _join() => _run(() async {
        final team = await widget.controller.api.redeemInvite(_link.text.trim());
        if (mounted) Navigator.of(context).pop(team);
      });

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final preview = _preview;
    final owner = preview == null ? null : TeamUser.from(preview['owner']);
    final granted = preview == null ? const <String>[] : (preview['permissions'] as List).map((item) => '$item').toList();
    return AlertDialog(
      title: Text(t.useInviteLink),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            TextField(
              controller: _link,
              enabled: preview == null,
              decoration: InputDecoration(labelText: t.inviteLinkField, hintText: t.inviteLinkFieldHint, isDense: true),
              onSubmitted: (_) => _check(),
            ),
            if (owner != null) ...<Widget>[
              const SizedBox(height: AppSpacing.md),
              Text(t.invitePreview(owner.displayName), style: TextStyle(color: palette.text, fontWeight: FontWeight.w600, height: 1.45)),
              const SizedBox(height: 4),
              Text(t.invitePreviewHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
              const SizedBox(height: AppSpacing.sm),
              _PermissionSummary(granted: granted),
            ],
            if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        if (preview == null)
          FilledButton(onPressed: _busy ? null : _check, child: Text(t.inviteCheck))
        else
          FilledButton(onPressed: _busy ? null : _join, child: Text(t.joinTeam)),
      ],
    );
  }
}
