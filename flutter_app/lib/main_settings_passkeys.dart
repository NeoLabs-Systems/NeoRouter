import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'src/auth/webauthn_client.dart';
import 'src/backend_client.dart';

/// Settings → Account → Passkeys. Hidden when the server does not offer them.
class PasskeysSection extends StatefulWidget {
  const PasskeysSection({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<PasskeysSection> createState() => _PasskeysSectionState();
}

class _PasskeysSectionState extends State<PasskeysSection> {
  List<Map<String, dynamic>>? _keys;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final keys = await widget.controller.api.passkeys();
      if (mounted) setState(() => _keys = keys);
    } catch (_) {
      // No list, no section.
    }
  }

  /// A dialog with a name and, for a new key, the password. It stays open and
  /// says why when the action fails.
  Future<void> _dialog({
    required String title,
    required String confirmLabel,
    required bool withPassword,
    required Future<List<Map<String, dynamic>>> Function(String name, String password) action,
    String name = '',
  }) async {
    final keys = await showDialog<List<Map<String, dynamic>>>(
      context: context,
      builder: (_) => _PasskeyDialog(title: title, confirmLabel: confirmLabel, withPassword: withPassword, name: name, action: action),
    );
    if (keys != null && mounted) setState(() => _keys = keys);
  }

  Future<void> _add() {
    final t = AppL10n.of(context);
    final api = widget.controller.api;
    return _dialog(
      title: t.passkeyAdd,
      confirmLabel: t.twoFactorContinue,
      withPassword: true,
      action: (name, password) async {
        final request = await api.passkeyCreateOptions(password);
        final response = await widget.controller.webAuthn.createCredential(Map<String, dynamic>.from(request['options'] as Map));
        return api.addPasskey(request['challengeId'] as String, response, name);
      },
    );
  }

  Future<void> _rename(Map<String, dynamic> key) {
    final t = AppL10n.of(context);
    return _dialog(
      title: t.passkeyRename,
      confirmLabel: t.save,
      withPassword: false,
      name: '${key['label'] ?? ''}',
      action: (name, _) => widget.controller.api.renamePasskey(key['id'] as int, name),
    );
  }

  Future<void> _remove(Map<String, dynamic> key) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(
      context,
      title: t.passkeyRemoveTitle,
      message: t.passkeyRemoveConfirm('${key['label'] ?? ''}'),
      confirmLabel: t.delete,
      cancelLabel: t.cancel,
    );
    if (!confirmed) return;
    try {
      final keys = await widget.controller.api.removePasskey(key['id'] as int);
      if (mounted) setState(() => _keys = keys);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final keys = _keys;
    if (keys == null) return const SizedBox.shrink();
    final t = AppL10n.of(context);
    final supported = widget.controller.webAuthn.isSupported;
    return SettingsGroup(
      title: t.passkeysTitle,
      children: <Widget>[
        SettingsRow(
          label: t.passkeysTitle,
          description: supported ? t.passkeysHint : t.passkeysUnsupported,
          control: supported ? OutlinedButton(onPressed: _add, child: Text(t.passkeyAdd)) : null,
        ),
        for (final key in keys)
          ListRow(
            title: '${key['label'] ?? ''}',
            subtitle: key['lastUsedAt'] == null ? t.passkeyNeverUsed : t.passkeyLastUsed('${key['lastUsedAt']}'.split(RegExp('[T ]')).first),
            trailing: PopupMenuButton<bool>(
              icon: const Icon(Icons.more_horiz_rounded),
              onSelected: (rename) => rename ? _rename(key) : _remove(key),
              itemBuilder: (_) => <PopupMenuEntry<bool>>[
                PopupMenuItem<bool>(value: true, child: Text(t.passkeyRename)),
                PopupMenuItem<bool>(value: false, child: Text(t.delete)),
              ],
            ),
          ),
        const SizedBox(height: AppSpacing.xs),
      ],
    );
  }
}

class _PasskeyDialog extends StatefulWidget {
  const _PasskeyDialog({required this.title, required this.confirmLabel, required this.withPassword, required this.name, required this.action});

  final String title;
  final String confirmLabel;
  final bool withPassword;
  final String name;
  final Future<List<Map<String, dynamic>>> Function(String name, String password) action;

  @override
  State<_PasskeyDialog> createState() => _PasskeyDialogState();
}

class _PasskeyDialogState extends State<_PasskeyDialog> {
  late final TextEditingController _name = TextEditingController(text: widget.name);
  final TextEditingController _password = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final name = _name.text.trim();
    if (_busy || (widget.withPassword ? _password.text.isEmpty : name.isEmpty)) return;
    final t = AppL10n.of(context);
    setState(() {
      _busy = true;
      _error = null;
    });
    String? error;
    try {
      final keys = await widget.action(name, _password.text);
      if (mounted) Navigator.of(context).pop(keys);
      return;
    } on ApiException catch (failure) {
      error = failure.code == 'INVALID_PASSWORD' ? t.accountWrongPassword : failure.message;
    } on WebAuthnException catch (failure) {
      if (!failure.cancelled) error = failure.message;
    } catch (_) {
      error = t.serverUnreachable;
    }
    if (mounted) {
      setState(() {
        _busy = false;
        _error = error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 400,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            TextField(controller: _name, autofocus: true, onSubmitted: (_) => _submit(), decoration: InputDecoration(labelText: t.passkeyName)),
            if (widget.withPassword) ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              TextField(controller: _password, obscureText: true, onSubmitted: (_) => _submit(), decoration: InputDecoration(labelText: t.currentPassword)),
            ],
            if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _busy ? null : _submit, child: Text(widget.confirmLabel)),
      ],
    );
  }
}
