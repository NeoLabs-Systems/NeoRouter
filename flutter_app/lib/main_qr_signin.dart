import 'dart:async';

import 'package:flutter/material.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoAuthQrPlate, NeoQrPolling, NeoQrSignInCard, QrChallengeAction;

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

/// Off while there is no NeoRouter phone app to scan with: the sign-in screen
/// then leaves the code out, and so never asks the server for one.
const bool showQrSignIn = false;

/// The code above the password form that a signed-in phone approves. It asks
/// for a code and polls only while the code is on screen and the page visible.
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
      payload = json['qrPayload'] as String,
      expiresAt = DateTime.tryParse(json['expiresAt'] as String? ?? '');

  final String id;
  final String pollToken;
  final String payload;
  final DateTime? expiresAt;

  int get secondsLeft => expiresAt?.difference(DateTime.now()).inSeconds ?? 0;
}

class _QrSignInPanelState extends State<QrSignInPanel> {
  _Challenge? _challenge;
  bool _preparing = false;
  bool _longPoll = false;
  bool _declined = false;
  String? _scannedBy;
  String? _error;

  late final NeoQrPolling _polling = NeoQrPolling(poll: _poll, serverHoldsPolls: () => _longPoll, canPoll: () => mounted && _challenge != null);
  late final AppLifecycleListener _lifecycle = AppLifecycleListener(
    onShow: () {
      _foreground = true;
      _applyVisibility();
    },
    onHide: () {
      _foreground = false;
      _polling.pause();
    },
  );
  late final Timer _countdown;
  // The dialog that shows the code on narrow screens follows this state.
  final ValueNotifier<int> _revision = ValueNotifier<int>(0);
  ModalRoute<Object?>? _codeRoute;
  bool _inline = false;
  bool _dialogOpen = false;
  bool _foreground = true;

  bool get _onScreen => (_inline || _dialogOpen) && _foreground;

  @override
  void setState(VoidCallback fn) {
    super.setState(fn);
    _revision.value++;
  }

  String get _server => widget.controller.client.baseUrl;

  @override
  void initState() {
    super.initState();
    _lifecycle;
    _countdown = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted && _challenge != null) setState(() {});
    });
  }

  @override
  void dispose() {
    // Signing in replaces this screen; an open code dialog goes with it.
    final route = _codeRoute;
    if (route != null && route.isActive) {
      WidgetsBinding.instance.addPostFrameCallback((_) => route.navigator?.removeRoute(route));
    }
    _countdown.cancel();
    _lifecycle.dispose();
    _polling.dispose();
    super.dispose();
  }

  /// Polls the code while it can be seen, asking for one first if needed.
  void _applyVisibility() {
    if (!mounted) return;
    if (!_onScreen) {
      _polling.pause();
    } else if (_challenge != null) {
      _polling.start();
    } else if (!_preparing && !_declined && _error == null) {
      unawaited(_prepare());
    }
  }

  /// The card reports this during layout, when asking for a code is too early.
  void _setInline(bool inline) {
    if (_inline == inline) return;
    _inline = inline;
    WidgetsBinding.instance.addPostFrameCallback((_) => _applyVisibility());
  }

  Future<void> _openCode() async {
    _dialogOpen = true;
    _applyVisibility();
    final t = AppL10n.of(context);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        _codeRoute = ModalRoute.of(dialogContext);
        return AlertDialog(
          title: Text(t.qrSignIn),
          content: SizedBox(
            width: 300,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Text(t.qrSignInHint),
                const SizedBox(height: AppSpacing.md),
                ValueListenableBuilder<int>(
                  valueListenable: _revision,
                  builder: (_, _, _) => NeoAuthQrPlate(size: 240, payload: _challenge?.payload, preparing: _preparing),
                ),
              ],
            ),
          ),
          actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.cancel))],
        );
      },
    );
    _codeRoute = null;
    _dialogOpen = false;
    _applyVisibility();
  }

  Future<void> _prepare() async {
    if (!mounted) return;
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
      if (_onScreen) _polling.start(fresh: true);
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
    if (scannedBy != null) {
      return Column(
        children: <Widget>[
          Text(t.qrSignInScanned(scannedBy), textAlign: TextAlign.center, style: style),
          TextButton(onPressed: _cancelScan, child: Text(t.cancel)),
        ],
      );
    }
    final challenge = _challenge;
    final seconds = challenge?.secondsLeft ?? 0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        NeoQrSignInCard(
          palette: neoLabsPaletteOf(context),
          payload: challenge?.payload,
          preparing: _preparing,
          title: t.qrSignIn,
          instructions: t.qrSignInHint,
          status: seconds > 0 ? t.qrRefreshesIn('$seconds') : t.qrWaitingForCode,
          refreshLabel: t.qrSignInNewCode,
          openLabel: t.qrShowCode,
          onRefresh: () => unawaited(_prepare()),
          onOpen: () => unawaited(_openCode()),
          onLayout: _setInline,
        ),
        if (_declined) ...<Widget>[
          const SizedBox(height: AppSpacing.sm),
          Text(t.qrSignInDeclined, textAlign: TextAlign.center, style: style),
        ],
        if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
      ],
    );
  }
}
