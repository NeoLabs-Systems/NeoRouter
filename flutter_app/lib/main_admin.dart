import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

enum _AdminTab { users, server, metadata }

extension on _AdminTab {
  IconData get icon => switch (this) {
        _AdminTab.users => Icons.group_outlined,
        _AdminTab.server => Icons.dns_outlined,
        _AdminTab.metadata => Icons.auto_awesome_outlined,
      };

  String label(AppL10n t) => switch (this) {
        _AdminTab.users => t.users,
        _AdminTab.server => t.serverTitle,
        _AdminTab.metadata => t.metadataSources,
      };
}

/// One thing an admin might look for and the tab that has it.
class _AdminSearchEntry {
  const _AdminSearchEntry(this.title, this.tab, this.keywords);

  final String title;
  final _AdminTab tab;
  final String keywords;

  bool matches(String query) {
    final haystack = '${title.toLowerCase()} $keywords';
    return query.split(RegExp(r'\s+')).where((word) => word.isNotEmpty).every(haystack.contains);
  }
}

/// Server administration in NeoAgent's layout: page title, search, tab chips.
class AdminScreen extends StatefulWidget {
  const AdminScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<AdminScreen> createState() => _AdminScreenState();
}

class _AdminScreenState extends State<AdminScreen> {
  final TextEditingController _search = TextEditingController();
  final TextEditingController _aaKey = TextEditingController();
  _AdminTab _tab = _AdminTab.users;
  List<AppUser> _users = <AppUser>[];
  Map<String, dynamic>? _settings;
  Map<String, dynamic>? _info;
  bool _loading = true;
  bool _refreshing = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _search.dispose();
    _aaKey.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.users(), api.adminSettings(), api.info()]);
      setState(() {
        _users = results[0] as List<AppUser>;
        _settings = results[1] as Map<String, dynamic>;
        _info = results[2] as Map<String, dynamic>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _guard(Future<void> Function() action) async {
    try {
      await action();
      await _load();
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  List<_AdminSearchEntry> _index(AppL10n t) => <_AdminSearchEntry>[
        _AdminSearchEntry(t.users, _AdminTab.users, 'accounts konten people email username admin role delete disable'),
        _AdminSearchEntry(t.registrationOpenLabel, _AdminTab.users, 'registration registrierung signup sign-up open close'),
        _AdminSearchEntry(t.serverTitle, _AdminTab.server, 'version endpoint url capabilities server'),
        _AdminSearchEntry(t.metadataSources, _AdminTab.metadata, 'models.dev openrouter prices preise context intelligence'),
        _AdminSearchEntry(t.aaKey, _AdminTab.metadata, 'artificial analysis benchmark key api'),
      ];

  void _open(_AdminTab tab) => setState(() {
        _tab = tab;
        _search.clear();
      });

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final query = _search.text.trim().toLowerCase();
    final results = query.isEmpty ? const <_AdminSearchEntry>[] : _index(t).where((entry) => entry.matches(query)).toList();
    return ListView(
      padding: pagePadding(context),
      children: <Widget>[
        ScreenHeader(title: t.navAdmin, description: t.adminDescription),
        TextField(
          controller: _search,
          onChanged: (_) => setState(() {}),
          onSubmitted: (_) {
            if (results.isNotEmpty) _open(results.first.tab);
          },
          decoration: InputDecoration(
            isDense: true,
            hintText: t.adminSearchHint,
            prefixIcon: Icon(Icons.search, size: 18, color: palette.textMuted),
            suffixIcon: _search.text.isEmpty
                ? null
                : IconButton(icon: Icon(Icons.close, size: 18, color: palette.textMuted), onPressed: () => setState(_search.clear)),
          ),
        ),
        const SizedBox(height: 14),
        if (query.isNotEmpty)
          results.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.search_off, title: t.nothingMatches, message: t.adminSearchHint))
              : SettingsCardList(
                  children: <Widget>[
                    for (final entry in results)
                      ListTile(
                        leading: Icon(entry.tab.icon, color: palette.accent),
                        title: Text(entry.title),
                        subtitle: Text(entry.tab.label(t), style: TextStyle(color: palette.textMuted, fontSize: 12)),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => _open(entry.tab),
                      ),
                  ],
                )
        else ...<Widget>[
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[for (final tab in _AdminTab.values) _AdminTabChip(icon: tab.icon, label: tab.label(t), active: tab == _tab, onTap: () => _open(tab))],
          ),
          const SizedBox(height: 20),
          AsyncBody(loading: _loading, error: _error, onRetry: _load, child: _body(t, palette)),
        ],
      ],
    );
  }

  Widget _body(AppL10n t, NeoRouterPalette palette) {
    switch (_tab) {
      case _AdminTab.users:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            SettingsGroup(
              title: t.registrationTitle,
              children: <Widget>[
                SettingsRow(
                  label: t.registrationOpenLabel,
                  description: t.registrationOpenHint,
                  control: Switch(
                    value: _settings?['registrationOpen'] == true,
                    onChanged: (value) => _guard(() => widget.controller.api.updateAdminSettings(<String, Object?>{'registrationOpen': value})),
                  ),
                ),
              ],
            ),
            SettingsGroup(
              title: t.users,
              description: t.usersCount('${_users.length}'),
              children: <Widget>[for (final user in _users) _userRow(t, palette, user)],
            ),
          ],
        );
      case _AdminTab.server:
        final capabilities = ((_info?['capabilities'] as List?) ?? const <Object>[]).map((item) => '${(item as Map)['label']}').join(', ');
        return SettingsGroup(
          title: t.serverTitle,
          description: t.serverInfoHint,
          children: <Widget>[
            SettingsRow(label: t.versionLabel, control: Text('${_info?['version'] ?? '—'}', style: GoogleFonts.geistMono(color: palette.textSoft, fontSize: 13))),
            SettingsRow(label: t.endpointTitle, description: t.endpointHint, control: SelectableText('${_info?['endpoint'] ?? '—'}', style: GoogleFonts.geistMono(color: palette.textSoft, fontSize: 13))),
            SettingsRow(label: t.capabilitiesLabel, control: Text(capabilities, style: TextStyle(color: palette.textSoft, fontSize: 13))),
          ],
        );
      case _AdminTab.metadata:
        final metadata = Map<String, dynamic>.from((_settings?['metadata'] as Map?) ?? <String, Object>{});
        final fetchedAt = Map<String, dynamic>.from((metadata['fetchedAt'] as Map?) ?? <String, Object>{});
        String fetched(String source) => fetchedAt[source] == null ? t.notFetched : formatTime(DateTime.parse(fetchedAt[source] as String).toLocal());
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            SettingsGroup(
              title: t.metadataSources,
              description: t.metadataHint,
              trailing: FilledButton.icon(
                onPressed: _refreshing
                    ? null
                    : () async {
                        final messenger = ScaffoldMessenger.of(context);
                        setState(() => _refreshing = true);
                        await _guard(() async {
                          await widget.controller.api.refreshMetadata();
                          messenger.showSnackBar(SnackBar(content: Text(t.metadataRefreshed), behavior: SnackBarBehavior.floating));
                        });
                        if (mounted) setState(() => _refreshing = false);
                      },
                icon: _refreshing ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.cloud_download_outlined, size: 18),
                label: Text(t.refreshMetadata),
              ),
              children: <Widget>[
                SettingsRow(label: 'models.dev', control: MetaPill(label: fetched('modelsDev'), tone: fetchedAt['modelsDev'] == null ? PillTone.neutral : PillTone.success)),
                SettingsRow(label: 'OpenRouter', control: MetaPill(label: fetched('openrouter'), tone: fetchedAt['openrouter'] == null ? PillTone.neutral : PillTone.success)),
                SettingsRow(
                  label: 'Artificial Analysis',
                  description: metadata['artificialAnalysisConfigured'] == true ? t.aaConfigured : null,
                  control: MetaPill(label: fetched('artificialAnalysis'), tone: fetchedAt['artificialAnalysis'] == null ? PillTone.neutral : PillTone.success),
                ),
              ],
            ),
            SettingsGroup(
              title: t.aaKey,
              children: <Widget>[
                Padding(
                  padding: const EdgeInsets.all(20),
                  child: Row(
                    children: <Widget>[
                      Expanded(child: TextField(controller: _aaKey, obscureText: true, decoration: InputDecoration(labelText: t.aaKey, isDense: true))),
                      const SizedBox(width: AppSpacing.sm),
                      OutlinedButton(
                        onPressed: () => _guard(() async {
                          await widget.controller.api.updateAdminSettings(<String, Object?>{'artificialAnalysisKey': _aaKey.text.trim().isEmpty ? null : _aaKey.text.trim()});
                          _aaKey.clear();
                        }),
                        child: Text(t.save),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ],
        );
    }
  }

  Widget _userRow(AppL10n t, NeoRouterPalette palette, AppUser user) {
    final self = user.id == widget.controller.user?.id;
    final initial = user.displayName.trim().isEmpty ? '?' : user.displayName.trim()[0].toUpperCase();
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Row(
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
                Text('${user.displayName}${self ? ' (${t.you})' : ''}', style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text)),
                const SizedBox(height: 2),
                Text('${user.username} · ${user.email}', style: TextStyle(color: palette.textSoft, fontSize: 13)),
              ],
            ),
          ),
          if (user.disabled) ...<Widget>[MetaPill(label: t.disabled, tone: PillTone.error), const SizedBox(width: 8)],
          MetaPill(label: user.isAdmin ? t.roleAdmin : t.roleUser, tone: user.isAdmin ? PillTone.accent : PillTone.neutral),
          if (!self)
            PopupMenuButton<String>(
              onSelected: (value) async {
                if (value == 'delete') {
                  final confirmed = await confirmAction(context, title: t.delete, message: t.deleteUserConfirm(user.username), confirmLabel: t.delete, cancelLabel: t.cancel);
                  if (confirmed) await _guard(() => widget.controller.api.deleteUser(user.id));
                  return;
                }
                await _guard(() => widget.controller.api.updateUser(
                      user.id,
                      disabled: value == 'toggle' ? !user.disabled : null,
                      role: value == 'role' ? (user.isAdmin ? 'user' : 'admin') : null,
                    ));
              },
              itemBuilder: (_) => <PopupMenuEntry<String>>[
                PopupMenuItem<String>(value: 'toggle', child: Text(user.disabled ? t.enableUser : t.disableUser)),
                PopupMenuItem<String>(value: 'role', child: Text(user.isAdmin ? t.makeUser : t.makeAdmin)),
                PopupMenuItem<String>(value: 'delete', child: Text(t.delete)),
              ],
            ),
        ],
      ),
    );
  }
}

/// Pill-shaped tab chip, copied from NeoAgent's admin console.
class _AdminTabChip extends StatelessWidget {
  const _AdminTabChip({required this.icon, required this.label, required this.active, required this.onTap});

  final IconData icon;
  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final radius = BorderRadius.circular(999);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
          decoration: BoxDecoration(
            borderRadius: radius,
            color: active ? palette.surfaceRaised : Colors.transparent,
            border: Border.all(color: active ? palette.borderStrong : palette.border),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(icon, size: 15, color: active ? palette.text : palette.textMuted),
              const SizedBox(width: 7),
              Text(label, style: GoogleFonts.geist(fontSize: 12.5, fontWeight: FontWeight.w600, color: active ? palette.text : palette.textSoft)),
            ],
          ),
        ),
      ),
    );
  }
}
