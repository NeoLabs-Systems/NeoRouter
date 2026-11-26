import 'dart:async';

import 'package:flutter/material.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoAuthQrPlate, NeoQrPolling, QrChallengeAction;

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

/// The code a signed-out screen shows so a signed-in phone can approve it.
/// It asks for a code and polls only while it is on screen and visible.
class QrSignInPanel extends StatefulWidget {
  const QrSignInPanel({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<QrSignInPanel> createState() => _QrSignInPanelState();
}

class _Challenge {
  _Challenge(Map<String, dynamic> json)
    : id = json['challengeId'] as String,
      pollToken = json['pollToken'] as String,
      payload = json['qrPayload'] as String;

  final String id;
  final String pollToken;
  final String payload;
}

class _QrSignInPanelState extends State<QrSignInPanel> {
  _Challenge? _challenge;
  bool _preparing = false;
  bool _longPoll = false;
  bool _declined = false;
  String? _scannedBy;
  String? _error;

  late final NeoQrPolling _polling = NeoQrPolling(poll: _poll, serverHoldsPolls: () => _longPoll, canPoll: () => mounted && _challenge != null);
  late final AppLifecycleListener _lifecycle = AppLifecycleListener(onShow: () => _polling.start(), onHide: _polling.pause);

  String get _server => widget.controller.client.baseUrl;

  @override
  void initState() {
    super.initState();
    _lifecycle;
    unawaited(_prepare());
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _polling.dispose();
    super.dispose();
  }

  Future<void> _prepare() async {
    _polling.pause();
    setState(() {
      _preparing = true;
      _challenge = null;
      _scannedBy = null;
      _error = null;
      _declined = false;
    });
    try {
      final result = await widget.controller.auth.createQrChallenge(_server, <String, dynamic>{'platform': 'web', 'appMode': 'web'});
      if (!mounted) return;
      setState(() => _challenge = _Challenge(result));
      _polling.start(fresh: true);
    } catch (error) {
      if (mounted) setState(() => _error = _describe(error));
    } finally {
      if (mounted) setState(() => _preparing = false);
    }
  }

  String _describe(Object error) => error is ApiException ? error.message : AppL10n.of(context).serverUnreachable;

  Future<Map<String, dynamic>> _action(_Challenge challenge, QrChallengeAction action, {String? knownStatus}) =>
      widget.controller.auth.qrChallengeAction(_server, challenge.id, challenge.pollToken, action, knownStatus: knownStatus);

  /// Null when the code was replaced or finished while the check waited.
  Future<bool?> _poll() async {
    final challenge = _challenge;
    if (challenge == null) return null;
    try {
      final status = await _action(challenge, QrChallengeAction.status, knownStatus: _scannedBy == null ? 'pending' : 'scanned');
      if (!mounted || _challenge != challenge) return null;
      _longPoll = status['longPoll'] == true;
      if (_error != null) setState(() => _error = null);
      switch (status['status']) {
        case 'approved':
          await _claim(challenge);
          return null;
        case 'scanned':
          final scanner = status['scannedBy'] as Map?;
          setState(() => _scannedBy = scanner?['label'] as String? ?? '');
        case 'declined':
          _polling.pause();
          setState(() {
            _challenge = null;
            _declined = true;
          });
          return null;
        case 'expired' || 'claimed':
          unawaited(_prepare());
          return null;
      }
      return true;
    } catch (error) {
      if (!mounted || _challenge != challenge) return null;
      setState(() => _error = _describe(error));
      return false;
    }
  }

  // An approval is claimed once; after a failed claim a new code is needed.
  Future<void> _claim(_Challenge challenge) async {
    _polling.pause();
    setState(() => _challenge = null);
    final signedIn = await widget.controller.claimQrLogin(() => _action(challenge, QrChallengeAction.claim));
    if (!signedIn && mounted) await _prepare();
  }

  // Gives up on a scanned code, so a late approval cannot sign this screen in.
  Future<void> _cancelScan() async {
    final challenge = _challenge;
    _polling.pause();
    if (challenge != null) {
      try {
        await _action(challenge, QrChallengeAction.cancel);
      } catch (_) {
        // The old code expires on its own; a fresh one replaces it either way.
      }
    }
    if (mounted) await _prepare();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final style = TextStyle(color: neoRouterPaletteOf(context).textSoft, fontSize: 13, height: 1.45);
    final scannedBy = _scannedBy;
    return Column(
      children: <Widget>[
        if (scannedBy != null) ...<Widget>[
          Text(t.qrSignInScanned(scannedBy), textAlign: TextAlign.center, style: style),
          TextButton(onPressed: _cancelScan, child: Text(t.cancel)),
        ] else if (_declined)
          Text(t.qrSignInDeclined, textAlign: TextAlign.center, style: style)
        else ...<Widget>[
          NeoAuthQrPlate(size: 180, payload: _challenge?.payload, preparing: _preparing),
          const SizedBox(height: AppSpacing.sm),
          Text(t.qrSignInHint, textAlign: TextAlign.center, style: style),
        ],
        if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
        if (!_preparing && scannedBy == null && (_declined || _challenge == null && _error != null))
          TextButton(onPressed: _prepare, child: Text(t.qrSignInNewCode)),
      ],
    );
  }
}
