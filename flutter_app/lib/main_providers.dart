import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:url_launcher/url_launcher.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_dashboard.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> _categoryOrder = <String>['free', 'plan', 'aggregator', 'paid', 'search', 'local', 'custom', 'community'];

String categoryLabel(AppL10n t, String category) => switch (category) {
  'free' => t.categoryFree,
  'aggregator' => t.categoryAggregator,
  'paid' => t.categoryPaid,
  'local' => t.categoryLocal,
  'custom' => t.categoryCustom,
  'search' => t.categorySearch,
  'plan' => t.categoryPlan,
  _ => t.categoryCommunity,
};

enum _ProvidersTab { connected, limits }

/// Connections: the provider accounts NeoRouter may use, and their limits.
class ProvidersScreen extends StatefulWidget {
  const ProvidersScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ProvidersScreen> createState() => _ProvidersScreenState();
}

class _ProvidersScreenState extends State<ProvidersScreen> {
  List<ProviderAccount> _accounts = <ProviderAccount>[];
  List<ProviderDefinition> _definitions = <ProviderDefinition>[];
  final Set<String> _busy = <String>{};
  bool _loading = true;
  String? _error;
  _ProvidersTab _tab = _ProvidersTab.connected;

  @override
  void initState() {
    super.initState();
    _load();
    WidgetsBinding.instance.addPostFrameCallback((_) => _showOAuthResult());
  }

  void _showOAuthResult() {
    final controller = widget.controller;
    if (!mounted || (controller.oauthStatus == null && controller.oauthError == null)) return;
    final t = AppL10n.of(context);
    showSnack(context, controller.oauthError != null ? t.openRouterFailed(controller.oauthError!) : t.openRouterConnected);
    controller.consumeOAuthResult();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.accounts(), if (_definitions.isEmpty) api.definitions()]);
      setState(() {
        _accounts = results[0] as List<ProviderAccount>;
        if (results.length > 1) _definitions = results[1] as List<ProviderDefinition>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _run(String accountId, Future<String?> Function() action) async {
    setState(() => _busy.add(accountId));
    try {
      final message = await action();
      if (mounted && message != null) showSnack(context, message);
      await _load();
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _busy.remove(accountId));
    }
  }

  Future<void> _add() async {
    final definition = await showDialog<ProviderDefinition>(
      context: context,
      builder: (_) => _ProviderGallery(definitions: _definitions),
    );
    if (definition == null || !mounted) return;
    final added = await showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition),
    );
    if (added != null && mounted) {
      showSnack(context, added);
      await _load();
    }
  }

  // A second key for the same provider: NeoRouter spreads requests over both.
  Future<void> _addAnother(ProviderAccount account) async {
    final definition = _definitions.where((item) => item.id == account.definitionId).firstOrNull;
    if (definition == null) return;
    final added = await showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition),
    );
    if (added != null && mounted) {
      showSnack(context, added);
      await _load();
    }
  }

  Future<void> _edit(ProviderAccount account) async {
    final definition = _definitions.where((item) => item.id == account.definitionId).firstOrNull;
    if (definition == null) return;
    final saved = await showDialog<String>(
      context: context,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition, account: account),
    );
    if (saved != null && mounted) {
      showSnack(context, saved);
      await _load();
    }
  }

  Future<void> _action(ProviderAccount account, _AccountAction action) async {
    final t = AppL10n.of(context);
    final api = widget.controller.api;
    switch (action) {
      case _AccountAction.check:
        await _run(account.id, () async {
          final result = await api.validateAccount(account.id);
          return (result['validation'] as Map?)?['message'] as String? ?? t.statusHealthy;
        });
      case _AccountAction.sync:
        await _run(account.id, () async {
          final result = await api.syncAccount(account.id);
          return t.syncedModels('${(result['sync'] as Map?)?['models'] ?? 0}');
        });
      case _AccountAction.balance:
        await _run(account.id, () async {
          final items = ((await api.probeQuota(account.id))['quota'] as List).cast<Map>();
          if (items.isEmpty) return t.quotaProbeEmpty;
          return items.map((item) => t.quotaProbeResult(metricLabel(t, '${item['metric']}'), '${item['remaining']}')).join(' · ');
        });
      case _AccountAction.edit:
        await _edit(account);
      case _AccountAction.another:
        await _addAnother(account);
      case _AccountAction.remove:
        final confirmed = await confirmAction(
          context,
          title: t.removeProvider,
          message: t.removeProviderConfirm(account.label),
          confirmLabel: t.removeProvider,
          cancelLabel: t.cancel,
        );
        if (confirmed) {
          await _run(account.id, () async {
            await api.deleteAccount(account.id);
            return null;
          });
        }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final canManage = controller.can('providers.manage');
    // The limits tab reads usage, which a team member may not be allowed to see.
    final showLimits = controller.can('activity.view');
    return PageScaffold(
      header: ScreenHeader(
        title: t.providersTitle,
        description: t.providersDescription,
        trailing: canManage
            ? FilledButton.icon(onPressed: _definitions.isEmpty ? null : _add, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.addProvider))
            : null,
      ),
      children: <Widget>[
        if (_accounts.isNotEmpty && showLimits)
          PageTabs<_ProvidersTab>(
            tabs: <({_ProvidersTab value, String label, int? count})>[
              (value: _ProvidersTab.connected, label: t.tabConnected, count: _accounts.length),
              (value: _ProvidersTab.limits, label: t.tabLimits, count: null),
            ],
            selected: _tab,
            onChanged: (value) => setState(() => _tab = value),
          ),
        if (_tab == _ProvidersTab.limits && _accounts.isNotEmpty && showLimits)
          _LimitsView(controller: widget.controller)
        else
          AsyncBody(
            loading: _loading,
            error: _error,
            onRetry: _load,
            child: _accounts.isEmpty
                ? AppCard(
                    child: EmptyState(
                      icon: Icons.link_rounded,
                      title: t.noProvidersTitle,
                      message: t.noProvidersMessage,
                      action: canManage
                          ? FilledButton.icon(
                              onPressed: _definitions.isEmpty ? null : _add,
                              icon: const Icon(Icons.add_rounded, size: 18),
                              label: Text(t.addProvider),
                            )
                          : null,
                    ),
                  )
                : SettingsCardList(
                    children: <Widget>[
                      for (final account in _accounts)
                        _AccountRow(
                          account: account,
                          busy: _busy.contains(account.id),
                          showCreator: controller.showCreators,
                          canManage: canManage,
                          canDelete: controller.can('providers.delete'),
                          onToggle: (enabled) => _run(account.id, () async {
                            await widget.controller.api.updateAccount(account.id, <String, Object?>{'enabled': enabled});
                            return null;
                          }),
                          onAction: (action) => _action(account, action),
                        ),
                    ],
                  ),
          ),
      ],
    );
  }
}

enum _AccountAction { check, sync, balance, edit, another, remove }

/// One connection: who, how many models, whether it is free and working.
class _AccountRow extends StatelessWidget {
  const _AccountRow({
    required this.account,
    required this.busy,
    required this.onToggle,
    required this.onAction,
    this.showCreator = false,
    this.canManage = true,
    this.canDelete = true,
  });

  final ProviderAccount account;
  final bool busy;
  final bool showCreator;
  final bool canManage;
  final bool canDelete;
  final ValueChanged<bool> onToggle;
  final ValueChanged<_AccountAction> onAction;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (statusText, tone) = accountStatus(t, account);
    final billing = account.billingMode == 'plan'
        ? t.accountPrepaidPlan
        : account.billingMode == 'plan_paid'
        ? t.accountPlanThenPaid
        : account.billingMode != 'free_tier'
        ? t.accountPaidPlan
        : !account.billingVerifiable
        ? t.accountFreePlan
        : switch (account.billingState) {
            'free' => t.accountFreeVerified,
            'paid' => t.accountBillingActive,
            _ => t.accountNotVerified,
          };
    final problem = account.enabled && account.status != 'healthy' && account.statusMessage != null ? account.statusMessage : null;
    return Opacity(
      opacity: account.enabled ? 1 : 0.6,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 8, 16),
        child: Row(
          children: <Widget>[
            ProviderAvatar(label: account.label, size: 38),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    account.label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text),
                  ),
                  const SizedBox(height: 3),
                  Text(
                    <String>[
                      t.homeModelsLine('${account.modelCount}', '${account.freeModelCount}'),
                      billing,
                      if (showCreator) createdByLabel(t, account.createdBy),
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13, color: palette.textMuted),
                  ),
                  if (problem != null) ...<Widget>[
                    const SizedBox(height: 4),
                    Text(
                      problem,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 12.5, color: palette.error),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 12),
            if (MediaQuery.sizeOf(context).width >= 700) ...<Widget>[StatusLabel(label: statusText, tone: tone), const SizedBox(width: 16)] else ...<Widget>[
              ToneDot(tone: tone),
              const SizedBox(width: 10),
            ],
            if (busy)
              const Padding(
                padding: EdgeInsets.all(14),
                child: SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)),
              )
            else
              Switch(value: account.enabled, onChanged: canManage ? onToggle : null),
            if (canManage || canDelete)
              PopupMenuButton<_AccountAction>(
                tooltip: t.moreActions,
                enabled: !busy,
                icon: const Icon(Icons.more_horiz_rounded, size: 20),
                onSelected: onAction,
                itemBuilder: (context) => <PopupMenuEntry<_AccountAction>>[
                  if (canManage) ...<PopupMenuEntry<_AccountAction>>[
                    _menuItem(_AccountAction.check, Icons.verified_user_outlined, t.revalidate),
                    _menuItem(_AccountAction.sync, Icons.sync_rounded, t.syncModels),
                    _menuItem(_AccountAction.balance, Icons.account_balance_wallet_outlined, t.probeQuota),
                    _menuItem(_AccountAction.edit, Icons.edit_outlined, t.editProvider),
                    _menuItem(_AccountAction.another, Icons.add_rounded, t.addAnotherAccount),
                  ],
                  if (canManage && canDelete) const PopupMenuDivider(),
                  if (canDelete) _menuItem(_AccountAction.remove, Icons.delete_outline_rounded, t.removeProvider, destructive: palette.error),
                ],
              ),
          ],
        ),
      ),
    );
  }

  static PopupMenuItem<_AccountAction> _menuItem(_AccountAction value, IconData icon, String label, {Color? destructive}) => PopupMenuItem<_AccountAction>(
    value: value,
    height: 40,
    child: Row(
      children: <Widget>[
        Icon(icon, size: 17, color: destructive),
        const SizedBox(width: 12),
        Text(label, style: TextStyle(fontSize: 13.5, color: destructive)),
      ],
    ),
  );
}

class _ProviderGallery extends StatefulWidget {
  const _ProviderGallery({required this.definitions});

  final List<ProviderDefinition> definitions;

  @override
  State<_ProviderGallery> createState() => _ProviderGalleryState();
}

class _ProviderGalleryState extends State<_ProviderGallery> {
  String _query = '';

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final query = _query.toLowerCase();
    final matches = widget.definitions.where(
      (item) => query.isEmpty || item.label.toLowerCase().contains(query) || item.id.contains(query) || item.description.toLowerCase().contains(query),
    );
    return Dialog(
      insetPadding: const EdgeInsets.all(16),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560, maxHeight: 640),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 22, 12, 0),
              child: Row(
                children: <Widget>[
                  Expanded(child: Text(t.chooseProvider, style: displayTitleStyle(palette, size: 18))),
                  IconButton(onPressed: () => Navigator.of(context).pop(), icon: const Icon(Icons.close_rounded, size: 20)),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 4, 24, 0),
              child: Text(t.chooseProviderHint, style: TextStyle(color: palette.textSoft, fontSize: 13.5)),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 16, 24, 8),
              child: TextField(
                autofocus: true,
                decoration: InputDecoration(prefixIcon: const Icon(Icons.search_rounded, size: 20), hintText: t.searchProviders),
                onChanged: (value) => setState(() => _query = value),
              ),
            ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.fromLTRB(12, 0, 12, 16),
                children: <Widget>[
                  for (final category in _categoryOrder)
                    if (matches.any((item) => item.category == category)) ...<Widget>[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(12, 14, 12, 6),
                        child: Text(categoryLabel(t, category).toUpperCase(), style: sectionEyebrowStyle(palette)),
                      ),
                      for (final definition in matches.where((item) => item.category == category))
                        ListRow(
                          onTap: () => Navigator.of(context).pop(definition),
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
                          leading: ProviderAvatar(label: definition.label, size: 32),
                          title: definition.label,
                          subtitle: definition.description,
                          trailing: definition.freeTierAvailable || definition.hasAlwaysFreeModels ? MetaPill(label: t.providerFreeTag) : null,
                        ),
                    ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Create (account == null) or edit a provider account.
class _ProviderForm extends StatefulWidget {
  const _ProviderForm({required this.controller, required this.definition, this.account});

  final NeoRouterController controller;
  final ProviderDefinition definition;
  final ProviderAccount? account;

  @override
  State<_ProviderForm> createState() => _ProviderFormState();
}

class _ProviderFormState extends State<_ProviderForm> {
  late final TextEditingController _label = TextEditingController(text: widget.account?.label ?? widget.definition.label);
  late final TextEditingController _slug = TextEditingController(text: widget.account?.slug ?? widget.definition.id.replaceAll('_', '-'));
  late final TextEditingController _secret = TextEditingController();
  late final TextEditingController _baseUrl = TextEditingController(text: widget.account?.baseUrl ?? widget.definition.baseUrl);
  late final Map<String, TextEditingController> _fields = <String, TextEditingController>{
    for (final field in widget.definition.fields) field.key: TextEditingController(text: '${(widget.account?.raw['options'] as Map?)?[field.key] ?? ''}'),
  };
  late final Map<String, dynamic> _savedOptions = Map<String, dynamic>.from((widget.account?.raw['options'] as Map?) ?? <String, Object>{});
  late final TextEditingController _authHeader = TextEditingController(text: '${_savedOptions['authHeader'] ?? ''}');
  late final TextEditingController _modelsPath = TextEditingController(text: '${_savedOptions['modelsPath'] ?? ''}');
  late final TextEditingController _manualModels = TextEditingController(text: ((_savedOptions['manualModels'] as List?) ?? const <Object>[]).join('\n'));
  late final TextEditingController _headers = TextEditingController(
    text: ((_savedOptions['headers'] as Map?) ?? const <String, Object>{}).entries.map((entry) => '${entry.key}: ${entry.value}').join('\n'),
  );
  late String _billing = widget.account?.billingMode ?? widget.definition.defaultBillingMode;
  bool _saving = false;
  bool _probing = false;
  bool _connecting = false;
  bool _advanced = false;
  Map<String, dynamic>? _probe;
  String? _error;

  bool get _editing => widget.account != null;
  bool get _custom => widget.definition.editableBaseUrl;

  @override
  void dispose() {
    for (final controller in <TextEditingController>[_label, _slug, _secret, _baseUrl, _authHeader, _modelsPath, _manualModels, _headers, ..._fields.values]) {
      controller.dispose();
    }
    super.dispose();
  }

  Map<String, Object> _options() {
    final options = <String, Object>{
      for (final entry in _fields.entries)
        if (entry.value.text.trim().isNotEmpty) entry.key: entry.value.text.trim(),
    };
    if (_custom) {
      final manual = _manualModels.text.split(RegExp(r'[\n,]')).map((line) => line.trim()).where((line) => line.isNotEmpty).toList();
      if (manual.isNotEmpty) options['manualModels'] = manual;
      if (_authHeader.text.trim().isNotEmpty) options['authHeader'] = _authHeader.text.trim();
      if (_modelsPath.text.trim().isNotEmpty) options['modelsPath'] = _modelsPath.text.trim();
    }
    return options;
  }

  Map<String, String>? _headerMap() {
    if (!_custom) return null;
    final headers = <String, String>{};
    for (final line in _headers.text.split('\n')) {
      final index = line.indexOf(':');
      if (index > 0) headers[line.substring(0, index).trim()] = line.substring(index + 1).trim();
    }
    return headers;
  }

  Future<void> _runProbe() async {
    final t = AppL10n.of(context);
    setState(() {
      _probing = true;
      _error = null;
    });
    try {
      final result = await widget.controller.api.probeProvider(<String, Object?>{
        'definitionId': widget.definition.id,
        if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
        if (_custom) 'baseUrl': _baseUrl.text.trim(),
        'options': _options(),
        'headers': _headerMap(),
      });
      setState(() => _probe = result);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _probing = false);
    }
  }

  Future<void> _connectOpenRouter() async {
    setState(() {
      _connecting = true;
      _error = null;
    });
    try {
      final url = await widget.controller.api.startOpenRouterConnect(Uri.base.toString());
      await launchUrl(Uri.parse(url), webOnlyWindowName: '_self');
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _connecting = false);
    }
  }

  Future<void> _submit() async {
    final t = AppL10n.of(context);
    setState(() {
      _saving = true;
      _error = null;
    });
    final options = _options();
    final headers = _headerMap();
    try {
      if (_editing) {
        await widget.controller.api.updateAccount(widget.account!.id, <String, Object?>{
          'label': _label.text.trim(),
          'slug': _slug.text.trim(),
          if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
          if (widget.definition.editableBaseUrl) 'baseUrl': _baseUrl.text.trim(),
          'options': options,
          'headers': headers,
          'billingMode': _billing,
        });
        if (mounted) Navigator.of(context).pop(t.saved);
        return;
      }
      final result = await widget.controller.api.createAccount(<String, Object?>{
        'definitionId': widget.definition.id,
        // Unchanged defaults are left to the server, which numbers a second
        // account of the same provider ("Groq 2", groq-2).
        if (_label.text.trim() != widget.definition.label) 'label': _label.text.trim(),
        if (_slug.text.trim() != widget.definition.id.replaceAll('_', '-')) 'slug': _slug.text.trim(),
        if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
        if (widget.definition.editableBaseUrl) 'baseUrl': _baseUrl.text.trim(),
        if (options.isNotEmpty) 'options': options,
        'headers': headers,
        'billingMode': _billing,
      });
      final validation = Map<String, dynamic>.from(result['validation'] as Map);
      final sync = result['sync'] as Map?;
      final message = validation['status'] == 'healthy' && sync?['models'] != null
          ? t.providerAdded('${sync!['models']}')
          : t.providerAddedUnverified('${validation['message'] ?? sync?['error'] ?? ''}');
      if (mounted) Navigator.of(context).pop(message);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final definition = widget.definition;
    const gap = SizedBox(height: AppSpacing.sm);
    return AlertDialog(
      title: Row(
        children: <Widget>[
          ProviderAvatar(label: definition.label, size: 32),
          const SizedBox(width: 12),
          Expanded(child: Text(_editing ? widget.account!.label : definition.label)),
        ],
      ),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (definition.description.isNotEmpty) Text(definition.description, style: TextStyle(color: palette.textSoft, height: 1.45)),
              if (definition.id == 'openrouter' && !_editing) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                FilledButton.icon(
                  onPressed: _connecting ? null : _connectOpenRouter,
                  icon: _connecting
                      ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.login_rounded, size: 18),
                  label: Text(t.connectOpenRouter),
                ),
                const SizedBox(height: 6),
                Text(t.connectOpenRouterHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                const SizedBox(height: AppSpacing.md),
                Row(
                  children: <Widget>[
                    Expanded(child: Divider(color: palette.border)),
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 10),
                      child: Text(t.orManualKey, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                    ),
                    Expanded(child: Divider(color: palette.border)),
                  ],
                ),
              ],
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: _secret,
                obscureText: true,
                autofocus: !_editing && definition.id != 'openrouter',
                decoration: InputDecoration(
                  labelText: _editing
                      ? t.apiKeyKeep
                      : definition.keyOptional
                      ? t.apiKeyOptional
                      : t.apiKey,
                ),
              ),
              if (definition.keyUrl != null)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: () => launchUrl(Uri.parse(definition.keyUrl!)),
                    icon: const Icon(Icons.open_in_new_rounded, size: 15),
                    label: Text(t.getKeyAt(definition.label)),
                  ),
                ),
              for (final field in definition.fields) ...<Widget>[
                gap,
                TextField(
                  controller: _fields[field.key],
                  decoration: InputDecoration(labelText: field.label, hintText: field.placeholder),
                ),
              ],
              if (_custom) ...<Widget>[
                gap,
                TextField(
                  controller: _baseUrl,
                  decoration: InputDecoration(labelText: t.baseUrl, hintText: 'https://…/v1'),
                ),
              ],
              const SizedBox(height: AppSpacing.lg),
              Text(
                t.billingMode,
                style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
              ),
              const SizedBox(height: AppSpacing.xs),
              for (final (mode, title, hint) in <(String, String, String)>[
                if (definition.freeTierAvailable) ('free_tier', t.billingOptionFree, t.billingOptionFreeHint),
                if (definition.planAvailable) ('plan', t.billingOptionPlan, definition.planNote ?? t.billingOptionPlanHint),
                if (definition.planAvailable) ('plan_paid', t.billingOptionPlanPaid, t.billingOptionPlanPaidHint),
                ('paid', t.billingOptionPaid, t.billingOptionPaidHint),
              ])
                OptionCard(title: title, description: hint, selected: _billing == mode, onTap: () => setState(() => _billing = mode)),
              if (_billing == 'free_tier' && definition.freeTierAvailable) ...<Widget>[
                const SizedBox(height: AppSpacing.xs),
                Text(t.billingFreeWarning, style: TextStyle(color: palette.textSoft, fontSize: 12.5, height: 1.45)),
              ] else if (definition.hasAlwaysFreeModels) ...<Widget>[
                const SizedBox(height: AppSpacing.xs),
                Text(t.billingAlwaysFreeNote, style: TextStyle(color: palette.textSoft, fontSize: 12.5, height: 1.45)),
              ],
              if (definition.billingNote.isNotEmpty) ...<Widget>[
                const SizedBox(height: 6),
                Text(definition.billingNote, style: TextStyle(color: palette.textMuted, fontSize: 12.5, height: 1.45)),
              ],
              const SizedBox(height: AppSpacing.md),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _advanced = !_advanced),
                  icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.advancedOptions),
                ),
              ),
              if (_advanced) ...<Widget>[
                TextField(
                  controller: _label,
                  decoration: InputDecoration(labelText: t.displayNameLabel),
                ),
                gap,
                TextField(
                  controller: _slug,
                  onChanged: (_) => setState(() {}),
                  decoration: InputDecoration(labelText: t.slugLabel, helperText: t.slugHint(_slug.text.isEmpty ? '…' : _slug.text)),
                ),
                if (_custom) ...<Widget>[
                  gap,
                  TextField(
                    controller: _authHeader,
                    decoration: InputDecoration(labelText: t.authHeaderLabel, hintText: 'x-api-key'),
                  ),
                  gap,
                  TextField(
                    controller: _headers,
                    minLines: 1,
                    maxLines: 4,
                    decoration: InputDecoration(labelText: t.customHeaders, hintText: 'X-Org: my-team'),
                  ),
                  gap,
                  TextField(
                    controller: _modelsPath,
                    decoration: InputDecoration(labelText: t.modelsPathLabel, hintText: '/models'),
                  ),
                  gap,
                  TextField(
                    controller: _manualModels,
                    minLines: 2,
                    maxLines: 6,
                    decoration: InputDecoration(labelText: t.manualModels, helperText: t.manualModelsHint, helperMaxLines: 2),
                  ),
                ],
              ],
              if (_custom) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                Align(
                  alignment: Alignment.centerLeft,
                  child: OutlinedButton.icon(
                    onPressed: _probing ? null : _runProbe,
                    icon: _probing
                        ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Icon(Icons.wifi_tethering_rounded, size: 16),
                    label: Text(t.testConnection),
                  ),
                ),
                if (_probe != null) _ProbeResult(result: _probe!),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(
          onPressed: _saving ? null : _submit,
          child: _saving ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(_editing ? t.save : t.checkAndAdd),
        ),
      ],
    );
  }
}

class _ProbeResult extends StatelessWidget {
  const _ProbeResult({required this.result});

  final Map<String, dynamic> result;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final validation = Map<String, dynamic>.from((result['validation'] as Map?) ?? <String, Object>{});
    final models = ((result['models'] as List?) ?? const <Object>[]).cast<Map>();
    final status = validation['status'] as String? ?? 'unknown';
    final ok = status == 'healthy' && result['modelsError'] == null;
    final message = ok
        ? t.probeOk('${models.length}')
        : result['modelsError'] != null
        ? t.modelsFetchFailed('${result['modelsError']}')
        : '${validation['message'] ?? t.statusError}';
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ok ? InlineSuccess(message: message) : InlineError(message: message),
          if (models.isNotEmpty) ...<Widget>[
            const SizedBox(height: AppSpacing.xs),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 120),
              child: SingleChildScrollView(
                child: Text(
                  models.take(120).map((model) => '${model['id']}').join(', '),
                  style: GoogleFonts.geistMono(fontSize: 12, color: palette.textMuted, height: 1.5),
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

String metricLabel(AppL10n t, String metric) => switch (metric) {
  'rpm' => t.metricRpm,
  'rpd' => t.metricRpd,
  'tpm' => t.metricTpm,
  'tpd' => t.metricTpd,
  'balance' || 'credits_usd' => t.balance,
  'plan_requests' => t.metricPlanRequests,
  'plan_5h' => t.metricPlan5h,
  'plan_week' => t.metricPlanWeek,
  'plan_month' => t.metricPlanMonth,
  _ => metric,
};

/// How much of each provider's allowance is used, with reset times.
class _LimitsView extends StatefulWidget {
  const _LimitsView({required this.controller});

  final NeoRouterController controller;

  @override
  State<_LimitsView> createState() => _LimitsViewState();
}

class _LimitsViewState extends State<_LimitsView> {
  List<Map<String, dynamic>> _accounts = <Map<String, dynamic>>[];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final accounts = await widget.controller.api.quotas();
      setState(() => _accounts = accounts);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AsyncBody(
      loading: _loading,
      error: _error,
      onRetry: _load,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(t.limitsDescription, style: TextStyle(color: neoRouterPaletteOf(context).textSoft, fontSize: 13.5, height: 1.45)),
          const SizedBox(height: AppSpacing.lg),
          for (final group in _accounts) ...<Widget>[_AccountLimits(group: group), const SizedBox(height: 28)],
        ],
      ),
    );
  }
}

/// Daily metrics matter to people; per-minute windows reset before anyone
/// could act on them and the router already avoids them.
const Set<String> _dailyMetrics = <String>{'rpd', 'tpd'};
const double _lowFraction = 0.8;

double _usedFraction(Map window) {
  final limit = (window['limit'] as num?)?.toDouble();
  if (limit == null || limit <= 0) return 0;
  final remaining = (window['remaining'] as num?)?.toDouble();
  final used = remaining == null ? (window['used'] as num? ?? 0).toDouble() : limit - remaining;
  return (used / limit).clamp(0, 1).toDouble();
}

/// Windows worth showing: daily ones, and any that is used up right now.
bool _worthShowing(Map window) => _dailyMetrics.contains('${window['metric']}') || _usedFraction(window) >= 1;

class _AccountLimits extends StatelessWidget {
  const _AccountLimits({required this.group});

  final Map<String, dynamic> group;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final account = Map<String, dynamic>.from(group['account'] as Map);
    final pool = (group['pool'] as List).cast<Map>().where(_worthShowing).toList();
    final probes = (group['probes'] as List).cast<Map>();
    final models = (group['models'] as List).cast<Map>();
    // The fullest window of each model decides whether it is running low.
    final low = <({Map model, Map window, double fraction})>[];
    for (final model in models) {
      final windows = (model['windows'] as List).cast<Map>().where(_worthShowing).toList();
      if (windows.isEmpty) continue;
      windows.sort((a, b) => _usedFraction(b).compareTo(_usedFraction(a)));
      final fraction = _usedFraction(windows.first);
      if (fraction >= _lowFraction) low.add((model: model, window: windows.first, fraction: fraction));
    }
    low.sort((a, b) => b.fraction.compareTo(a.fraction));
    final rows = <Widget>[
      // Plan windows get a usage bar; other probes (balances) stay plain values.
      for (final probe in probes.where((item) => '${item['metric']}'.startsWith('plan_') && item['limit'] != null))
        _WindowRow(title: metricLabel(t, '${probe['metric']}'), window: probe),
      for (final probe in probes.where((item) => !'${item['metric']}'.startsWith('plan_') || item['limit'] == null))
        SettingsRow(
          label: metricLabel(t, '${probe['metric']}'),
          control: Text('${probe['remaining'] ?? '—'}${probe['limit'] == null ? '' : ' / ${probe['limit']}'}', style: TextStyle(color: palette.text)),
        ),
      if (group['concurrency'] is Map)
        SettingsRow(
          label: t.limitConcurrent,
          control: Text(
            (group['concurrency'] as Map)['source'] == 'learned'
                ? t.limitConcurrentValue('${(group['concurrency'] as Map)['limit']}')
                : t.limitConcurrentDocumented('${(group['concurrency'] as Map)['limit']}'),
            style: TextStyle(color: palette.textSoft, fontSize: 13),
          ),
        ),
      for (final window in pool) _WindowRow(title: metricLabel(t, '${window['metric']}'), window: window),
      if (models.isNotEmpty)
        SettingsRow(
          label: t.limitsModelsRoom('${models.length - low.where((item) => item.fraction >= 1).length}', '${models.length}'),
          control: low.isEmpty ? Icon(Icons.check_rounded, size: 18, color: palette.success) : const SizedBox.shrink(),
        ),
      for (final item in low.take(6))
        _WindowRow(title: '${shortModelName('${item.model['id']}')} · ${metricLabel(t, '${item.window['metric']}')}', window: item.window),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: '${account['label']}'),
        if (rows.isEmpty)
          AppCard(
            child: Text(t.noQuotaData, style: TextStyle(color: palette.textSoft)),
          )
        else
          SettingsCardList(children: rows),
      ],
    );
  }
}

class _WindowRow extends StatelessWidget {
  const _WindowRow({required this.title, required this.window});

  final String title;
  final Map window;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final limit = (window['limit'] as num?)?.toDouble();
    final remaining = (window['remaining'] as num?)?.toDouble() ?? 0;
    final used = limit == null ? (window['used'] as num? ?? 0).toDouble() : (limit - remaining).clamp(0, limit).toDouble();
    final resetAt = window['resetAt'] as num?;
    final detail = <String>[
      limit == null ? formatCount(used) : t.usedOf(formatCount(used), formatCount(limit)),
      if (resetAt != null) t.limitResets(formatTime(DateTime.fromMillisecondsSinceEpoch(resetAt.toInt()))),
    ].join(' · ');
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13.5, color: palette.text),
                ),
              ),
              Text(detail, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
            ],
          ),
          const SizedBox(height: 8),
          UsageBar(fraction: limit == null || limit == 0 ? 0 : used / limit, height: 4),
        ],
      ),
    );
  }
}
