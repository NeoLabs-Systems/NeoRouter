import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

enum _Step { loading, ask, approved, declined, failed }

/// Opened by scanning a sign-in code: shows what is asking and waits for a tap.
class QrApproveScreen extends StatefulWidget {
  const QrApproveScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<QrApproveScreen> createState() => _QrApproveScreenState();
}

class _QrApproveScreenState extends State<QrApproveScreen> {
  static const Map<String, dynamic> _metadata = <String, dynamic>{'platform': 'web', 'appMode': 'web'};

  late final _link = widget.controller.pendingQrApproval!;
  _Step _step = _Step.loading;
  Map<String, dynamic> _request = const <String, dynamic>{};
  bool _busy = false;
  String? _error;

  String get _server => widget.controller.client.baseUrl;

  @override
  void initState() {
    super.initState();
    _run(() async {
      _request = await widget.controller.auth.resolveQr(_server, _link.challengeId, _link.secret, _metadata);
    }, then: _Step.ask);
  }

  Future<void> _run(Future<void> Function() action, {required _Step then}) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      if (mounted) setState(() => _step = then);
    } catch (error) {
      if (mounted) {
        setState(() {
          _step = _Step.failed;
          _error = error is ApiException ? error.message : AppL10n.of(context).serverUnreachable;
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _approve() => _run(() => widget.controller.auth.approveQr(_server, _link.challengeId, _link.secret, _metadata), then: _Step.approved);

  void _decline() => _run(() => widget.controller.auth.declineQr(_server, _link.challengeId, _link.secret), then: _Step.declined);

  @override
  Widget build(BuildContext context) {
    return AuthBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(14),
              child: GlassSurface(
                width: 440,
                padding: const EdgeInsets.all(AppSpacing.xl),
                child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: _body(context)),
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
    final soft = TextStyle(color: palette.textSoft, height: 1.45);
    if (_step == _Step.loading) return const <Widget>[Center(child: CircularProgressIndicator())];
    if (_step != _Step.ask) {
      return <Widget>[
        if (_error != null) InlineError(message: _error!) else Text(_step == _Step.approved ? t.qrApproveDone : t.qrApproveDeclined, style: soft),
        const SizedBox(height: AppSpacing.lg),
        FilledButton(onPressed: widget.controller.finishQrApproval, child: Text(t.connectOpenNeoRouter)),
      ];
    }
    final device = (_request['requestedDevice'] as Map?)?['label'] as String? ?? '';
    final location = _request['requestLocation'] as Map?;
    final where = <String>[location?['label'] as String? ?? '', location?['ipAddress'] as String? ?? ''].where((part) => part.isNotEmpty).toSet().join(' · ');
    return <Widget>[
      Text(t.qrApproveTitle, style: displayTitleStyle(palette, size: 24)),
      const SizedBox(height: AppSpacing.md),
      if (device.isNotEmpty) Text(device, style: TextStyle(color: palette.text, fontWeight: FontWeight.w600)),
      if (where.isNotEmpty) Text(where, style: soft),
      const SizedBox(height: AppSpacing.md),
      Text(t.qrApproveWarning(widget.controller.user?.username ?? ''), style: soft),
      const SizedBox(height: AppSpacing.lg),
      Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: <Widget>[
          OutlinedButton(onPressed: _busy ? null : _decline, child: Text(t.connectDeny)),
          const SizedBox(width: AppSpacing.sm),
          FilledButton(onPressed: _busy ? null : _approve, child: Text(t.qrApproveAction)),
        ],
      ),
    ];
  }
}
