import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:qr_flutter/qr_flutter.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

enum _SettingsPage { account, appearance, connect }

enum _SettingsScope { you, app }

extension on _SettingsPage {
  _SettingsScope get scope => this == _SettingsPage.account ? _SettingsScope.you : _SettingsScope.app;

  IconData get icon => switch (this) {
        _SettingsPage.account => Icons.person_outline,
        _SettingsPage.appearance => Icons.palette_outlined,
        _SettingsPage.connect => Icons.code_rounded,
      };

  String label(AppL10n t) => switch (this) {
        _SettingsPage.account => t.account,
        _SettingsPage.appearance => t.appearance,
        _SettingsPage.connect => t.connectApps,
      };

  String description(AppL10n t) => switch (this) {
        _SettingsPage.account => t.settingsAccountDescription,
        _SettingsPage.appearance => t.settingsAppearanceDescription,
        _SettingsPage.connect => t.settingsConnectDescription,
      };
}

extension on _SettingsScope {
  String label(AppL10n t) => this == _SettingsScope.app ? t.settingsScopeApp : t.settingsScopeYou;

  String? hint(AppL10n t) => this == _SettingsScope.app ? t.settingsScopeAppHint : t.settingsScopeYouHint;
}

/// Settings in the NeoAgent layout: a page rail beside the open page on wide
/// screens, a page list that opens one page at a time on a phone.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final TextEditingController _search = TextEditingController();
  _SettingsPage? _page;

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  List<_SettingsPage> _pages(AppL10n t) {
    final query = _search.text.trim().toLowerCase();
    return _SettingsPage.values
        .where((page) => query.isEmpty || '${page.label(t)} ${page.description(t)}'.toLowerCase().contains(query))
        .toList();
  }

  void _open(_SettingsPage page) => setState(() {
        _page = page;
        _search.clear();
      });

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final t = AppL10n.of(context);
    final pages = _pages(t);
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth >= 760) {
          final shown = _page ?? _SettingsPage.account;
          return Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Container(
                width: 268,
                decoration: BoxDecoration(color: palette.surface, border: Border(right: BorderSide(color: palette.border))),
                child: _SettingsNav(pages: pages, search: _search, onSearch: () => setState(() {}), selected: shown, compact: false, onOpen: _open),
              ),
              Expanded(child: _SettingsPageView(key: ValueKey<_SettingsPage>(shown), controller: widget.controller, page: shown)),
            ],
          );
        }
        final page = _page;
        if (page == null) {
          return _SettingsNav(pages: pages, search: _search, onSearch: () => setState(() {}), selected: null, compact: true, onOpen: _open);
        }
        return PopScope(
          canPop: false,
          onPopInvokedWithResult: (didPop, _) {
            if (!didPop) setState(() => _page = null);
          },
          child: _SettingsPageView(
            key: ValueKey<_SettingsPage>(page),
            controller: widget.controller,
            page: page,
            onBack: () => setState(() => _page = null),
          ),
        );
      },
    );
  }
}

class _SettingsNav extends StatelessWidget {
  const _SettingsNav({required this.pages, required this.search, required this.onSearch, required this.selected, required this.compact, required this.onOpen});

  final List<_SettingsPage> pages;
  final TextEditingController search;
  final VoidCallback onSearch;
  final _SettingsPage? selected;
  final bool compact;
  final ValueChanged<_SettingsPage> onOpen;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ListView(
      padding: compact ? pagePadding(context) : const EdgeInsets.fromLTRB(14, 26, 14, 24),
      children: <Widget>[
        Padding(
          padding: EdgeInsets.fromLTRB(compact ? 2 : 10, 0, 0, 14),
          child: Text(
            t.settingsTitle,
            style: compact ? displayTitleStyle(palette, size: 26) : TextStyle(fontSize: 20, fontWeight: FontWeight.w700, letterSpacing: -0.3, color: palette.text),
          ),
        ),
        TextField(
          controller: search,
          onChanged: (_) => onSearch(),
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            isDense: true,
            hintText: t.settingsSearchHint,
            prefixIcon: Icon(Icons.search, size: 18, color: palette.textMuted),
            suffixIcon: search.text.isEmpty
                ? null
                : IconButton(
                    icon: Icon(Icons.close, size: 18, color: palette.textMuted),
                    onPressed: () {
                      search.clear();
                      onSearch();
                    },
                  ),
          ),
        ),
        for (final scope in _SettingsScope.values) ..._scopeSection(context, scope, pages.where((page) => page.scope == scope).toList()),
      ],
    );
  }

  List<Widget> _scopeSection(BuildContext context, _SettingsScope scope, List<_SettingsPage> pages) {
    if (pages.isEmpty) return const <Widget>[];
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final header = Padding(
      padding: EdgeInsets.fromLTRB(compact ? 4 : 10, 22, 4, 8),
      child: Text(
        scope.label(t).toUpperCase(),
        style: GoogleFonts.geistMono(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 1.4, color: palette.textMuted),
      ),
    );
    if (!compact) {
      return <Widget>[
        header,
        for (final page in pages) _SettingsNavItem(icon: page.icon, label: page.label(t), selected: page == selected, onTap: () => onOpen(page)),
      ];
    }
    return <Widget>[
      header,
      SettingsCardList(
        children: <Widget>[
          for (final page in pages)
            InkWell(
              onTap: () => onOpen(page),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                child: Row(
                  children: <Widget>[
                    Container(
                      width: 32,
                      height: 32,
                      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(9)),
                      child: Icon(page.icon, size: 17, color: palette.textSoft),
                    ),
                    const SizedBox(width: 12),
                    Expanded(child: Text(page.label(t), style: TextStyle(fontSize: 15, fontWeight: FontWeight.w500, color: palette.text))),
                    Icon(Icons.chevron_right_rounded, size: 20, color: palette.textMuted),
                  ],
                ),
              ),
            ),
        ],
      ),
    ];
  }
}

class _SettingsNavItem extends StatelessWidget {
  const _SettingsNavItem({required this.icon, required this.label, required this.selected, required this.onTap});

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: Material(
        color: selected ? palette.surfaceRaised : Colors.transparent,
        borderRadius: BorderRadius.circular(10),
        child: InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
            child: Row(
              children: <Widget>[
                Icon(icon, size: 18, color: selected ? palette.accent : palette.textMuted),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 14, fontWeight: selected ? FontWeight.w600 : FontWeight.w500, color: selected ? palette.text : palette.textSoft),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SettingsPageView extends StatelessWidget {
  const _SettingsPageView({super.key, required this.controller, required this.page, this.onBack});

  final NeoRouterController controller;
  final _SettingsPage page;

  /// Set on a phone, where the page covers the list.
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return ListView(
      padding: pagePadding(context),
      children: <Widget>[
        Align(
          alignment: Alignment.topLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 880),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                if (onBack != null)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: onBack,
                      icon: const Icon(Icons.arrow_back_ios_new_rounded, size: 16),
                      label: Text(t.settingsTitle),
                    ),
                  ),
                _SettingsPageHeader(page: page),
                const SizedBox(height: 26),
                switch (page) {
                  _SettingsPage.account => _AccountPage(controller: controller),
                  _SettingsPage.appearance => _AppearancePage(controller: controller),
                  _SettingsPage.connect => _ConnectPage(controller: controller),
                },
                const SizedBox(height: AppSpacing.xl),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// Scope, title and description of a page, plus who it applies to, as in
/// NeoAgent's settings page header.
class _SettingsPageHeader extends StatelessWidget {
  const _SettingsPageHeader({required this.page});

  final _SettingsPage page;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final compact = MediaQuery.sizeOf(context).width < 760;
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(page.scope.label(t).toUpperCase(), style: sectionEyebrowStyle(palette)),
        const SizedBox(height: 8),
        Text(page.label(t), style: displayTitleStyle(palette, size: compact ? 24 : 30)),
        const SizedBox(height: 8),
        Text(page.description(t), style: TextStyle(color: palette.textSoft, height: 1.5)),
      ],
    );
    final hint = page.scope.hint(t);
    final scope = hint == null
        ? null
        : Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
            decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(999), border: Border.all(color: palette.border)),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Icon(page.scope == _SettingsScope.you ? Icons.cloud_outlined : Icons.devices_outlined, size: 14, color: palette.textSoft),
                const SizedBox(width: 7),
                Flexible(child: Text(hint, style: TextStyle(color: palette.textSoft, fontSize: 12.5))),
              ],
            ),
          );
    final status = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(Icons.check_rounded, size: 15, color: palette.textMuted),
        const SizedBox(width: 6),
        Flexible(child: Text(page == _SettingsPage.account ? t.settingsSavedOnSubmit : t.settingsAppliesInstantly, style: TextStyle(color: palette.textMuted, fontSize: 12.5))),
      ],
    );
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 640) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              heading,
              const SizedBox(height: 14),
              Wrap(spacing: 14, runSpacing: 10, crossAxisAlignment: WrapCrossAlignment.center, children: <Widget>[?scope, status]),
            ],
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: <Widget>[
            Expanded(child: heading),
            const SizedBox(width: 24),
            Column(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: <Widget>[
                if (scope != null) ...<Widget>[scope, const SizedBox(height: 10)],
                status,
              ],
            ),
          ],
        );
      },
    );
  }
}

class _AppearancePage extends StatelessWidget {
  const _AppearancePage({required this.controller});

  final NeoRouterController controller;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return SettingsCardList(
      children: <Widget>[
        SettingsRow(
          label: t.themeLabel,
          description: t.themeHint,
          control: _SettingsChoice<ThemeMode>(
            value: controller.themeMode,
            options: <(ThemeMode, String)>[(ThemeMode.system, t.themeSystem), (ThemeMode.light, t.themeLight), (ThemeMode.dark, t.themeDark)],
            onChanged: controller.setThemeMode,
          ),
        ),
        SettingsRow(
          label: t.language,
          description: t.languageHint,
          control: _SettingsChoice<String>(
            value: controller.locale?.languageCode ?? 'system',
            options: <(String, String)>[('system', t.languageSystem), ('de', 'Deutsch'), ('en', 'English')],
            onChanged: (value) => controller.setLocale(value == 'system' ? null : Locale(value)),
          ),
        ),
      ],
    );
  }
}

class _AccountPage extends StatefulWidget {
  const _AccountPage({required this.controller});

  final NeoRouterController controller;

  @override
  State<_AccountPage> createState() => _AccountPageState();
}

class _AccountPageState extends State<_AccountPage> {
  late final TextEditingController _displayName = TextEditingController(text: widget.controller.user?.displayName ?? '');
  late final TextEditingController _email = TextEditingController(text: widget.controller.user?.email ?? '');
  final TextEditingController _currentPassword = TextEditingController();
  final TextEditingController _newPassword = TextEditingController();
  bool _passwordOpen = false;
  String? _profileError;
  String? _passwordError;

  @override
  void dispose() {
    _displayName.dispose();
    _email.dispose();
    _currentPassword.dispose();
    _newPassword.dispose();
    super.dispose();
  }

  Future<void> _saveProfile() async {
    final t = AppL10n.of(context);
    setState(() => _profileError = null);
    try {
      final user = await widget.controller.api.updateProfile(displayName: _displayName.text.trim(), email: _email.text.trim());
      widget.controller.updateUser(user);
      if (mounted) showSnack(context, t.saved);
    } on ApiException catch (error) {
      setState(() => _profileError = error.message);
    }
  }

  Future<void> _changePassword() async {
    final t = AppL10n.of(context);
    setState(() => _passwordError = null);
    if (_newPassword.text.length < 12) {
      setState(() => _passwordError = t.passwordTooShort);
      return;
    }
    try {
      await widget.controller.api.changePassword(_currentPassword.text, _newPassword.text);
      if (mounted) showSnack(context, t.passwordChanged);
      await widget.controller.logout();
    } on ApiException catch (error) {
      setState(() => _passwordError = error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.profileTitle),
        SettingsCardList(
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Wrap(
                    spacing: AppSpacing.sm,
                    runSpacing: AppSpacing.sm,
                    children: <Widget>[
                      SizedBox(width: 340, child: TextField(controller: _displayName, decoration: InputDecoration(labelText: t.displayName))),
                      SizedBox(width: 340, child: TextField(controller: _email, decoration: InputDecoration(labelText: t.email))),
                    ],
                  ),
                  if (_profileError != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _profileError!)],
                  const SizedBox(height: AppSpacing.md),
                  Align(alignment: Alignment.centerLeft, child: FilledButton(onPressed: _saveProfile, child: Text(t.save))),
                ],
              ),
            ),
          ],
        ),
        const SizedBox(height: 28),
        _TwoFactorSection(controller: widget.controller),
        const SizedBox(height: 28),
        SettingsCardList(
          children: <Widget>[
            SettingsRow(
              label: t.changePassword,
              description: t.passwordHint,
              control: _passwordOpen ? null : OutlinedButton(onPressed: () => setState(() => _passwordOpen = true), child: Text(t.changePassword)),
            ),
            if (_passwordOpen)
              Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    Wrap(
                      spacing: AppSpacing.sm,
                      runSpacing: AppSpacing.sm,
                      children: <Widget>[
                        SizedBox(width: 340, child: TextField(controller: _currentPassword, obscureText: true, decoration: InputDecoration(labelText: t.currentPassword))),
                        SizedBox(width: 340, child: TextField(controller: _newPassword, obscureText: true, decoration: InputDecoration(labelText: t.newPassword))),
                      ],
                    ),
                    if (_passwordError != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _passwordError!)],
                    const SizedBox(height: AppSpacing.md),
                    Wrap(
                      spacing: AppSpacing.sm,
                      runSpacing: AppSpacing.sm,
                      children: <Widget>[
                        OutlinedButton(
                          onPressed: () => setState(() {
                            _passwordOpen = false;
                            _passwordError = null;
                            _currentPassword.clear();
                            _newPassword.clear();
                          }),
                          child: Text(t.cancel),
                        ),
                        FilledButton(onPressed: _changePassword, child: Text(t.changePassword)),
                      ],
                    ),
                  ],
                ),
              ),
            SettingsRow(
              label: t.logout,
              description: t.signOutHint,
              control: OutlinedButton.icon(
                onPressed: widget.controller.logout,
                style: OutlinedButton.styleFrom(foregroundColor: palette.error),
                icon: const Icon(Icons.logout, size: 16),
                label: Text(t.logout),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

enum _TwoFactorStep { idle, password, scan, codes, manage }

enum _TwoFactorAction { disable, regenerate }

/// Authenticator-app sign-in: enable with password, scan, confirm a code, then
/// keep the one-time recovery codes.
class _TwoFactorSection extends StatefulWidget {
  const _TwoFactorSection({required this.controller});

  final NeoRouterController controller;

  @override
  State<_TwoFactorSection> createState() => _TwoFactorSectionState();
}

class _TwoFactorSectionState extends State<_TwoFactorSection> {
  final TextEditingController _password = TextEditingController();
  final TextEditingController _code = TextEditingController();
  Map<String, dynamic>? _status;
  _TwoFactorStep _step = _TwoFactorStep.idle;
  _TwoFactorAction _action = _TwoFactorAction.disable;
  String? _otpauthUrl;
  String? _manualKey;
  List<String> _recoveryCodes = const <String>[];
  String? _error;
  bool _busy = false;

  bool get _enabled => _status?['enabled'] == true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _password.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final status = await widget.controller.api.twoFactorStatus();
      if (mounted) setState(() => _status = status);
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    }
  }

  void _reset([_TwoFactorStep step = _TwoFactorStep.idle]) {
    setState(() {
      _step = step;
      _error = null;
      _password.clear();
      _code.clear();
    });
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _beginSetup() => _run(() async {
        final result = await widget.controller.api.twoFactorSetup(_password.text);
        setState(() {
          _otpauthUrl = result['otpauthUrl'] as String?;
          // Groups of four are easier to type into an authenticator app.
          _manualKey = (result['manualKey'] as String?)?.replaceAllMapped(RegExp(r'.{4}'), (m) => '${m[0]} ').trim();
          _password.clear();
          _step = _TwoFactorStep.scan;
        });
      });

  Future<void> _enable() => _run(() async {
        final result = await widget.controller.api.twoFactorEnable(_code.text.trim());
        setState(() {
          _recoveryCodes = (result['recoveryCodes'] as List<dynamic>).cast<String>();
          _status = result['status'] as Map<String, dynamic>?;
          _code.clear();
          _otpauthUrl = null;
          _manualKey = null;
          _step = _TwoFactorStep.codes;
        });
      });

  Future<void> _submitManage() => _run(() async {
        final api = widget.controller.api;
        final code = _code.text.trim();
        if (_action == _TwoFactorAction.disable) {
          final result = await api.twoFactorDisable(_password.text, code);
          setState(() => _status = result['status'] as Map<String, dynamic>? ?? <String, dynamic>{'enabled': false});
          _reset();
          if (mounted) showSnack(context, AppL10n.of(context).twoFactorDisabledNotice);
          return;
        }
        final result = await api.twoFactorRecoveryCodes(_password.text, code);
        setState(() {
          _recoveryCodes = (result['recoveryCodes'] as List<dynamic>).cast<String>();
          _status = result['status'] as Map<String, dynamic>?;
          _password.clear();
          _code.clear();
          _step = _TwoFactorStep.codes;
        });
      });

  Widget _form(List<Widget> fields, List<Widget> actions) {
    return Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ...fields,
          if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
          const SizedBox(height: AppSpacing.md),
          Wrap(spacing: AppSpacing.sm, runSpacing: AppSpacing.sm, children: actions),
        ],
      ),
    );
  }

  void _copy(String value) {
    Clipboard.setData(ClipboardData(text: value));
    showSnack(context, AppL10n.of(context).copied);
  }

  Widget _button(String label, Future<void> Function() onPressed) {
    return FilledButton(
      onPressed: _busy ? null : onPressed,
      child: _busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(label),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final cancel = OutlinedButton(onPressed: _busy ? null : () => _reset(), child: Text(t.cancel));
    final passwordField = SizedBox(
      width: 340,
      child: TextField(controller: _password, obscureText: true, decoration: InputDecoration(labelText: t.currentPassword)),
    );
    final remaining = _status?['recoveryCodesRemaining'] as int? ?? 0;

    final rows = <Widget>[
      SettingsRow(
        label: t.twoFactorTitle,
        description: _status == null ? null : (_enabled ? t.twoFactorEnabledHint('$remaining') : t.twoFactorDisabledHint),
        control: _status == null
            ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
            : _step != _TwoFactorStep.idle
            ? null
            : _enabled
            ? Wrap(
                spacing: AppSpacing.sm,
                runSpacing: AppSpacing.sm,
                children: <Widget>[
                  OutlinedButton(
                    onPressed: () {
                      _action = _TwoFactorAction.regenerate;
                      _reset(_TwoFactorStep.manage);
                    },
                    child: Text(t.twoFactorNewCodes),
                  ),
                  OutlinedButton(
                    onPressed: () {
                      _action = _TwoFactorAction.disable;
                      _reset(_TwoFactorStep.manage);
                    },
                    style: OutlinedButton.styleFrom(foregroundColor: palette.error),
                    child: Text(t.twoFactorDisable),
                  ),
                ],
              )
            : FilledButton(onPressed: () => _reset(_TwoFactorStep.password), child: Text(t.twoFactorEnable)),
      ),
    ];

    switch (_step) {
      case _TwoFactorStep.idle:
        if (_error != null && _status == null) {
          rows.add(Padding(padding: const EdgeInsets.all(20), child: InlineError(message: _error!)));
        }
      case _TwoFactorStep.password:
        rows.add(_form(
          <Widget>[
            Text(t.twoFactorPasswordPrompt, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.sm),
            Align(alignment: Alignment.centerLeft, child: passwordField),
          ],
          <Widget>[cancel, _button(t.twoFactorContinue, _beginSetup)],
        ));
      case _TwoFactorStep.scan:
        rows.add(_form(
          <Widget>[
            Text(t.twoFactorScanHint, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.md),
            Wrap(
              spacing: 24,
              runSpacing: AppSpacing.md,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                if (_otpauthUrl != null)
                  Container(
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(AppRadius.card)),
                    child: QrImageView(data: _otpauthUrl!, size: 168, backgroundColor: Colors.white),
                  ),
                SizedBox(
                  width: 300,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(t.twoFactorManualKey, style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: palette.textSoft)),
                      const SizedBox(height: 6),
                      Row(
                        children: <Widget>[
                          Expanded(child: SelectableText(_manualKey ?? '', style: GoogleFonts.geistMono(fontSize: 13, color: palette.text))),
                          IconButton(
                            tooltip: t.copy,
                            icon: const Icon(Icons.copy, size: 16),
                            onPressed: () => _copy(_manualKey ?? ''),
                          ),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.md),
                      TextField(
                        controller: _code,
                        keyboardType: TextInputType.number,
                        autofillHints: const <String>[AutofillHints.oneTimeCode],
                        onSubmitted: (_) => _enable(),
                        decoration: InputDecoration(labelText: t.twoFactorCode),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ],
          <Widget>[cancel, _button(t.twoFactorVerify, _enable)],
        ));
      case _TwoFactorStep.codes:
        rows.add(_form(
          <Widget>[
            Text(t.twoFactorCodesHint, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.md),
            Container(
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: palette.surfaceMuted,
                borderRadius: BorderRadius.circular(AppRadius.card),
                border: Border.all(color: palette.border),
              ),
              child: Wrap(
                spacing: 24,
                runSpacing: 8,
                children: <Widget>[
                  for (final code in _recoveryCodes)
                    SizedBox(width: 150, child: SelectableText(code, style: GoogleFonts.geistMono(fontSize: 13.5, color: palette.text))),
                ],
              ),
            ),
          ],
          <Widget>[
            OutlinedButton.icon(
              onPressed: () => _copy(_recoveryCodes.join('\n')),
              icon: const Icon(Icons.copy, size: 16),
              label: Text(t.copy),
            ),
            FilledButton(
              onPressed: () {
                _recoveryCodes = const <String>[];
                _reset();
              },
              child: Text(t.twoFactorCodesSaved),
            ),
          ],
        ));
      case _TwoFactorStep.manage:
        rows.add(_form(
          <Widget>[
            Text(
              _action == _TwoFactorAction.disable ? t.twoFactorDisablePrompt : t.twoFactorNewCodesPrompt,
              style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45),
            ),
            const SizedBox(height: AppSpacing.sm),
            Wrap(
              spacing: AppSpacing.sm,
              runSpacing: AppSpacing.sm,
              children: <Widget>[
                passwordField,
                SizedBox(
                  width: 340,
                  child: TextField(
                    controller: _code,
                    onSubmitted: (_) => _submitManage(),
                    decoration: InputDecoration(labelText: t.twoFactorCodeOrRecovery),
                  ),
                ),
              ],
            ),
          ],
          <Widget>[
            cancel,
            _button(_action == _TwoFactorAction.disable ? t.twoFactorDisable : t.twoFactorNewCodes, _submitManage),
          ],
        ));
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(
          title: t.securityTitle,
          trailing: _status == null
              ? null
              : MetaPill(
                  label: _enabled ? t.twoFactorOn : t.twoFactorOff,
                  tone: _enabled ? PillTone.success : PillTone.neutral,
                  icon: _enabled ? Icons.verified_user_outlined : Icons.shield_outlined,
                ),
        ),
        SettingsCardList(children: rows),
      ],
    );
  }
}

class _ConnectPage extends StatelessWidget {
  const _ConnectPage({required this.controller});

  final NeoRouterController controller;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final endpoint = controller.endpoint ?? 'http://127.0.0.1:4600/v1';
    final base = endpoint.replaceFirst(RegExp(r'/v1$'), '');
    final snippets = <(String, String)>[
      ('OpenAI SDK (Python)', 'from openai import OpenAI\nclient = OpenAI(base_url="$endpoint", api_key="nrt_…")\nclient.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hi"}])'),
      ('Anthropic SDK (Python)', 'from anthropic import Anthropic\nclient = Anthropic(base_url="$base", api_key="nrt_…")\nclient.messages.create(model="auto:smart", max_tokens=512, messages=[{"role": "user", "content": "Hi"}])'),
      ('curl', 'curl $endpoint/chat/completions \\\n  -H "Authorization: Bearer nrt_…" -H "Content-Type: application/json" \\\n  -d \'{"model":"auto:fast","messages":[{"role":"user","content":"Hi"}]}\''),
      ('Claude Code', 'ANTHROPIC_BASE_URL=$base ANTHROPIC_AUTH_TOKEN=nrt_… claude'),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.connectApps, description: t.connectAppsHint),
        SettingsCardList(children: <Widget>[for (final (label, code) in snippets) _Snippet(label: label, code: code)]),
      ],
    );
  }
}

/// A one-of-a-few choice, styled like NeoAgent's.
class _SettingsChoice<T extends Object> extends StatelessWidget {
  const _SettingsChoice({required this.value, required this.options, required this.onChanged});

  final T value;
  final List<(T, String)> options;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    return SegmentedButton<T>(
      segments: <ButtonSegment<T>>[
        for (final (option, label) in options) ButtonSegment<T>(value: option, label: Text(label)),
      ],
      selected: <T>{value},
      showSelectedIcon: false,
      onSelectionChanged: (selection) => onChanged(selection.first),
    );
  }
}

class _Snippet extends StatelessWidget {
  const _Snippet({required this.label, required this.code});

  final String label;
  final String code;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final t = AppL10n.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 8, 8, 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(child: Text(label, style: TextStyle(color: palette.text, fontSize: 14, fontWeight: FontWeight.w600))),
              IconButton(
                tooltip: t.copy,
                icon: const Icon(Icons.copy_rounded, size: 16),
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: code));
                  showSnack(context, t.copied);
                },
              ),
            ],
          ),
          Container(
            margin: const EdgeInsets.only(right: 12),
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.tag)),
            child: SelectableText(code, style: GoogleFonts.geistMono(fontSize: 12.5, height: 1.55, color: palette.textSoft)),
          ),
        ],
      ),
    );
  }
}
