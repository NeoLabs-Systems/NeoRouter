import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:url_launcher/url_launcher.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_keys.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

enum _ConnectStep { code, loading, ask, approved, denied, failed }

/// "Connect with NeoRouter": an app asked for a key and the user decides
/// what it may use. Opened from the link the app sent the user to
/// (`?connect=<request>`), or with an empty request to type the app's code.
class ConnectAppScreen extends StatefulWidget {
  const ConnectAppScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ConnectAppScreen> createState() => _ConnectAppScreenState();
}

class _ConnectAppScreenState extends State<ConnectAppScreen> {
  final TextEditingController _code = TextEditingController();
  final TextEditingController _budget = TextEditingController();
  final TextEditingController _rpm = TextEditingController();
  final TextEditingController _tokens = TextEditingController();
  String _tokenPeriod = 'monthly';
  _ConnectStep _step = _ConnectStep.loading;
  ConnectRequest? _request;
  String? _error;
  bool _busy = false;
  bool _free = true;
  bool _plan = true;
  bool _paid = false;
  String _period = 'monthly';

  NeoRouterController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    final reference = _controller.pendingConnect ?? '';
    if (_controller.connectError != null) {
      _step = _ConnectStep.failed;
      _error = _controller.connectError;
    } else if (reference.isEmpty) {
      _step = _ConnectStep.code;
    } else {
      _load(reference);
    }
  }

  @override
  void dispose() {
    _code.dispose();
    _budget.dispose();
    _rpm.dispose();
    _tokens.dispose();
    super.dispose();
  }

  Future<void> _load(String reference) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final request = await _controller.api.connectRequest(reference);
      setState(() {
        _request = request;
        _step = _ConnectStep.ask;
      });
    } on ApiException catch (error) {
      setState(() {
        _error = error.message;
        // A mistyped code can be corrected; an expired link cannot.
        if (_step != _ConnectStep.code) _step = _ConnectStep.failed;
      });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _answer(bool approve) async {
    final t = AppL10n.of(context);
    if (approve && !_free && !_plan && !_paid) {
      setState(() => _error = t.pickOneKind);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final redirect = await _controller.api.answerConnect(
        _request!.id,
        approve: approve,
        access: approve
            ? <String, Object?>{
                'allowFree': _free,
                'allowPlan': _plan,
                'allowPaid': _paid,
                'budgetUsd': _paid ? double.tryParse(_budget.text.trim().replaceAll(',', '.')) : null,
                'budgetPeriod': _period,
                'rpmLimit': int.tryParse(_rpm.text.trim()),
                'tokenLimit': _free || _plan ? int.tryParse(_tokens.text.trim().replaceAll(RegExp(r'[^0-9]'), '')) : null,
                'tokenPeriod': _tokenPeriod,
              }
            : null,
      );
      if (redirect != null) {
        // Back to the app, which exchanges the code for its key.
        await launchUrl(Uri.parse(redirect), webOnlyWindowName: '_self');
        return;
      }
      setState(() => _step = approve ? _ConnectStep.approved : _ConnectStep.denied);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  // Drops the connect link from the address so a reload opens NeoRouter.
  Future<void> _openNeoRouter() async {
    _controller.finishConnect();
    final clean = Uri.base.replace(queryParameters: <String, String>{});
    await launchUrl(Uri.parse(clean.toString().replaceFirst(RegExp(r'\?$'), '')), webOnlyWindowName: '_self');
  }

  @override
  Widget build(BuildContext context) {
    return AuthBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: LayoutBuilder(
            builder: (context, viewport) => SingleChildScrollView(
              child: ConstrainedBox(
                constraints: BoxConstraints(minHeight: viewport.maxHeight),
                child: Padding(
                  padding: EdgeInsets.all(viewport.maxWidth < AppBreakpoints.mobile ? 14 : 24),
                  child: Center(
                    child: EntranceMotion(
                      child: GlassSurface(
                        width: 480,
                        padding: const EdgeInsets.all(AppSpacing.xl),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: <Widget>[
                            const Center(child: BrandLockup(logoSize: 44, showName: false)),
                            const SizedBox(height: AppSpacing.md),
                            ..._body(context),
                          ],
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  List<Widget> _body(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final name = _request?.name ?? '';
    Widget title(String text) => Text(text, style: displayTitleStyle(palette, size: 24));
    Widget body(String text) => Text(text, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: palette.textSoft, height: 1.45));
    Widget openButton() => FilledButton(onPressed: _openNeoRouter, child: Text(t.connectOpenNeoRouter));
    switch (_step) {
      case _ConnectStep.loading:
        return const <Widget>[Center(child: Padding(padding: EdgeInsets.all(AppSpacing.lg), child: CircularProgressIndicator()))];
      case _ConnectStep.code:
        return <Widget>[
          title(t.connectCodeTitle),
          const SizedBox(height: AppSpacing.sm),
          body(t.connectCodeSubtitle),
          const SizedBox(height: AppSpacing.lg),
          TextField(
            controller: _code,
            autofocus: true,
            textCapitalization: TextCapitalization.characters,
            style: GoogleFonts.geistMono(fontSize: 18, letterSpacing: 2),
            decoration: InputDecoration(labelText: t.connectCodeField, hintText: 'ABCD-EFGH'),
            onSubmitted: (_) => _load(_code.text),
          ),
          if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
          const SizedBox(height: AppSpacing.lg),
          FilledButton(onPressed: _busy ? null : () => _load(_code.text), child: Text(t.connectCodeContinue)),
          const SizedBox(height: AppSpacing.sm),
          TextButton(onPressed: _openNeoRouter, child: Text(t.cancel)),
        ];
      case _ConnectStep.ask:
        return _askBody(context, t, palette, title, body);
      case _ConnectStep.approved:
        return <Widget>[title(t.connectDoneTitle(name)), const SizedBox(height: AppSpacing.sm), body(t.connectDoneBody), const SizedBox(height: AppSpacing.lg), openButton()];
      case _ConnectStep.denied:
        return <Widget>[title(t.connectDeniedTitle), const SizedBox(height: AppSpacing.sm), body(t.connectDeniedBody(name)), const SizedBox(height: AppSpacing.lg), openButton()];
      case _ConnectStep.failed:
        return <Widget>[
          title(t.connectFailedTitle),
          const SizedBox(height: AppSpacing.sm),
          if (_error != null) body(_error!),
          const SizedBox(height: AppSpacing.lg),
          openButton(),
        ];
    }
  }

  List<Widget> _askBody(BuildContext context, AppL10n t, NeoRouterPalette palette, Widget Function(String) title, Widget Function(String) body) {
    final request = _request!;
    final owner = _controller.spaceOwner;
    final allowed = _controller.can('keys.create');
    return <Widget>[
      title(t.connectTitle(request.name)),
      const SizedBox(height: AppSpacing.sm),
      body(t.connectSubtitle(request.name)),
      const SizedBox(height: AppSpacing.md),
      _Fact(
        icon: request.isDevice ? Icons.devices_other_outlined : (request.secure ? Icons.lock_outline_rounded : Icons.lock_open_rounded),
        text: request.isDevice ? t.connectDeviceHint : t.connectSendsTo(request.host ?? ''),
        warning: !request.isDevice && !request.secure ? t.connectInsecure : null,
      ),
      if (request.scopes.isNotEmpty) ...<Widget>[
        const SizedBox(height: AppSpacing.md),
        Text(t.connectWantsToUse, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
        const SizedBox(height: AppSpacing.xs),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: <Widget>[
            for (final scope in request.scopes) MetaPill(icon: capabilityInfo(scope).icon, label: capabilityInfo(scope).label(t)),
          ],
        ),
      ],
      const SizedBox(height: AppSpacing.lg),
      if (!allowed) ...<Widget>[
        InlineHint(message: t.connectNoPermission),
        const SizedBox(height: AppSpacing.md),
        FilledButton(onPressed: () => _controller.switchSpace(null), child: Text(t.mySpace)),
      ] else ...<Widget>[
        Text(t.keyAccessTitle, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
        const SizedBox(height: AppSpacing.xs),
        KeyAccessFields(
          free: _free,
          plan: _plan,
          paid: _paid,
          budget: _budget,
          period: _period,
          rpm: _rpm,
          tokens: _tokens,
          tokenPeriod: _tokenPeriod,
          onFree: (value) => setState(() => _free = value),
          onPlan: (value) => setState(() => _plan = value),
          onPaid: (value) => setState(() => _paid = value),
          onPeriod: (value) => setState(() => _period = value),
          onTokenPeriod: (value) => setState(() => _tokenPeriod = value),
        ),
        if (owner != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineHint(message: t.connectInSpace(t.spaceOf(owner.displayName)))],
        if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
        const SizedBox(height: AppSpacing.lg),
        Row(
          children: <Widget>[
            Expanded(child: OutlinedButton(onPressed: _busy ? null : () => _answer(false), child: Text(t.connectDeny))),
            const SizedBox(width: AppSpacing.sm),
            Expanded(child: FilledButton(onPressed: _busy ? null : () => _answer(true), child: Text(t.connectApprove))),
          ],
        ),
      ],
    ];
  }
}

/// One line about where the key goes, with an optional warning under it.
class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.text, this.warning});

  final IconData icon;
  final String text;
  final String? warning;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      padding: const EdgeInsets.all(AppSpacing.sm),
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(icon, size: 18, color: warning != null ? palette.warning : palette.textSoft),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(text, style: TextStyle(color: palette.text, fontSize: 13.5)),
                if (warning != null) ...<Widget>[
                  const SizedBox(height: 4),
                  Text(warning!, style: TextStyle(color: palette.warning, fontSize: 12.5, height: 1.4)),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
