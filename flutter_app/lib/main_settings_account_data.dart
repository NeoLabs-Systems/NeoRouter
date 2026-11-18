import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/web_media.dart';

/// Settings → Account → Your data: export everything, export the saved
/// provider keys (needs 2FA), delete the account. Each step asks for the
/// password again, and for a code when 2FA is on.
class AccountDataSection extends StatelessWidget {
  const AccountDataSection({super.key, required this.controller});

  final NeoRouterController controller;

  Future<bool> _twoFactorOn() async => (await controller.api.twoFactorStatus())['enabled'] == true;

  String _fileName(String kind) {
    final date = DateTime.now().toIso8601String().substring(0, 10);
    return 'neorouter-$kind-${controller.user?.username ?? 'account'}-$date.json';
  }

  void _save(Map<String, dynamic> data, String kind) {
    final bytes = Uint8List.fromList(utf8.encode(const JsonEncoder.withIndent('  ').convert(data)));
    downloadBytes(bytes, 'application/json', _fileName(kind));
  }

  Future<void> _exportData(BuildContext context) async {
    final t = AppL10n.of(context);
    final twoFactor = await _guard(context, _twoFactorOn);
    if (twoFactor == null || !context.mounted) return;
    final data = await _confirm(
      context,
      title: t.accountExportTitle,
      confirmLabel: t.accountExport,
      twoFactor: twoFactor,
      action: (password, code) => controller.api.exportAccountData(password, code),
    );
    if (data == null || !context.mounted) return;
    _save(data, 'account');
    showSnack(context, t.accountExported);
  }

  Future<void> _exportKeys(BuildContext context) async {
    final t = AppL10n.of(context);
    final twoFactor = await _guard(context, _twoFactorOn);
    if (twoFactor == null || !context.mounted) return;
    if (!twoFactor) {
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text(t.accountKeysTitle),
          content: SizedBox(width: 400, child: Text(t.accountKeysNeedTwoFactor, style: TextStyle(color: neoRouterPaletteOf(context).textSoft, height: 1.45))),
          actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.close))],
        ),
      );
      return;
    }
    final data = await _confirm(
      context,
      title: t.accountKeysTitle,
      message: t.accountKeysWarning,
      confirmLabel: t.accountExport,
      twoFactor: true,
      action: (password, code) => controller.api.exportSavedKeys(password, code ?? ''),
    );
    if (data == null || !context.mounted) return;
    _save(data, 'keys');
    showSnack(context, t.accountExported);
  }

  // A key file from "export saved API keys": shown, confirmed, then each key
  // is checked with its provider on the server.
  Future<void> _importKeys(BuildContext context) async {
    final t = AppL10n.of(context);
    final picked = await pickFile(accept: '.json,application/json');
    if (picked == null || !context.mounted) return;
    Map<String, dynamic>? file;
    try {
      final decoded = jsonDecode(utf8.decode(picked.bytes));
      if (decoded is Map<String, dynamic> && decoded['kind'] == 'neorouter-keys' && decoded['keys'] is List && (decoded['keys'] as List).isNotEmpty) file = decoded;
    } on FormatException {
      file = null;
    }
    if (file == null) {
      showSnack(context, t.accountImportNotAFile);
      return;
    }
    final keys = (file['keys'] as List).whereType<Map>().toList();
    final names = keys.map((key) => '${key['label'] ?? key['provider'] ?? key['definitionId']}').join(', ');
    final confirmed = await confirmAction(
      context,
      title: t.accountImportTitle,
      message: t.accountImportConfirm('${keys.length}', names),
      confirmLabel: t.accountImport,
      cancelLabel: t.cancel,
      destructive: false,
    );
    if (!confirmed || !context.mounted) return;
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _ImportProgressDialog(run: () => controller.api.importKeys(file!)),
    );
    if (result == null || !context.mounted) return;
    await showDialog<void>(context: context, builder: (_) => _ImportResultDialog(result: result));
  }

  Future<void> _delete(BuildContext context) async {
    final t = AppL10n.of(context);
    final twoFactor = await _guard(context, _twoFactorOn);
    if (twoFactor == null || !context.mounted) return;
    final done = await _confirm(
      context,
      title: t.accountDeleteTitle,
      message: t.accountDeleteConfirm,
      confirmLabel: t.accountDelete,
      twoFactor: twoFactor,
      destructive: true,
      action: (password, code) async {
        await controller.api.deleteMyAccount(password, code);
        return true;
      },
    );
    if (done != true || !context.mounted) return;
    showSnack(context, t.accountDeleted);
    await controller.logout();
  }

  Future<T?> _guard<T>(BuildContext context, Future<T> Function() action) async {
    try {
      return await action();
    } on ApiException catch (error) {
      if (context.mounted) showSnack(context, error.message);
      return null;
    }
  }

  Future<T?> _confirm<T>(
    BuildContext context, {
    required String title,
    required String confirmLabel,
    required bool twoFactor,
    required Future<T> Function(String password, String? code) action,
    String? message,
    bool destructive = false,
  }) => showDialog<T>(
    context: context,
    builder: (_) => _ConfirmIdentityDialog<T>(
      title: title,
      message: message,
      confirmLabel: confirmLabel,
      twoFactor: twoFactor,
      destructive: destructive,
      action: action,
    ),
  );

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return SettingsGroup(
      title: t.accountDataTitle,
      children: <Widget>[
        SettingsRow(
          label: t.accountExportTitle,
          description: t.accountExportHint,
          control: OutlinedButton(onPressed: () => _exportData(context), child: Text(t.accountExport)),
        ),
        SettingsRow(
          label: t.accountKeysTitle,
          description: t.accountKeysHint,
          control: OutlinedButton(onPressed: () => _exportKeys(context), child: Text(t.accountExport)),
        ),
        SettingsRow(
          label: t.accountImportTitle,
          description: t.accountImportHint,
          control: OutlinedButton(onPressed: () => _importKeys(context), child: Text(t.accountImport)),
        ),
        SettingsRow(
          label: t.accountDeleteTitle,
          description: t.accountDeleteHint,
          control: OutlinedButton(
            onPressed: () => _delete(context),
            style: OutlinedButton.styleFrom(foregroundColor: palette.error),
            child: Text(t.accountDelete),
          ),
        ),
      ],
    );
  }
}

/// Password (and code) again, then runs the action; wrong entries are shown
/// in the dialog so they can be corrected.
class _ConfirmIdentityDialog<T> extends StatefulWidget {
  const _ConfirmIdentityDialog({
    required this.title,
    required this.confirmLabel,
    required this.twoFactor,
    required this.destructive,
    required this.action,
    this.message,
  });

  final String title;
  final String? message;
  final String confirmLabel;
  final bool twoFactor;
  final bool destructive;
  final Future<T> Function(String password, String? code) action;

  @override
  State<_ConfirmIdentityDialog<T>> createState() => _ConfirmIdentityDialogState<T>();
}

class _ConfirmIdentityDialogState<T> extends State<_ConfirmIdentityDialog<T>> {
  final TextEditingController _password = TextEditingController();
  final TextEditingController _code = TextEditingController();
  bool _working = false;
  String? _error;

  @override
  void dispose() {
    _password.dispose();
    _code.dispose();
    super.dispose();
  }

  bool get _ready => _password.text.isNotEmpty && (!widget.twoFactor || _code.text.trim().length >= 6);

  String _errorText(AppL10n t, ApiException error) => switch (error.code) {
    'INVALID_PASSWORD' => t.accountWrongPassword,
    'INVALID_TWO_FACTOR' => t.accountWrongCode,
    'CONFIRM_LOCKED' => t.accountTooManyTries,
    'LAST_ADMIN' => t.accountLastAdmin,
    'TWO_FACTOR_REQUIRED' => t.accountKeysNeedTwoFactor,
    _ => error.message,
  };

  Future<void> _submit() async {
    if (!_ready || _working) return;
    final t = AppL10n.of(context);
    setState(() {
      _working = true;
      _error = null;
    });
    try {
      final result = await widget.action(_password.text, widget.twoFactor ? _code.text.trim() : null);
      if (mounted) Navigator.of(context).pop(result);
    } on ApiException catch (error) {
      if (mounted) {
        setState(() {
          _working = false;
          _error = _errorText(t, error);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final hint = TextStyle(color: palette.textSoft, height: 1.45);
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 400,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (widget.message != null) ...<Widget>[Text(widget.message!, style: hint), const SizedBox(height: AppSpacing.md)],
            Text(widget.twoFactor ? t.accountConfirmPasswordCode : t.accountConfirmPassword, style: hint),
            const SizedBox(height: AppSpacing.md),
            TextField(
              controller: _password,
              obscureText: true,
              autofocus: true,
              decoration: InputDecoration(labelText: t.currentPassword),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _submit(),
            ),
            if (widget.twoFactor) ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              TextField(
                controller: _code,
                decoration: InputDecoration(labelText: t.twoFactorCodeOrRecovery),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => _submit(),
              ),
            ],
            if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _working ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(
          onPressed: _ready && !_working ? _submit : null,
          style: widget.destructive ? FilledButton.styleFrom(backgroundColor: palette.error, foregroundColor: Colors.white) : null,
          child: _working
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : Text(widget.confirmLabel),
        ),
      ],
    );
  }
}

/// Runs the import while saying what is happening; closes with the result,
/// or with nothing after showing why it failed.
class _ImportProgressDialog extends StatefulWidget {
  const _ImportProgressDialog({required this.run});

  final Future<Map<String, dynamic>> Function() run;

  @override
  State<_ImportProgressDialog> createState() => _ImportProgressDialogState();
}

class _ImportProgressDialogState extends State<_ImportProgressDialog> {
  String? _error;

  @override
  void initState() {
    super.initState();
    widget.run().then((result) {
      if (mounted) Navigator.of(context).pop(result);
    }, onError: (Object error) {
      if (mounted) setState(() => _error = error is ApiException ? error.message : '$error');
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return AlertDialog(
      title: Text(t.accountImportTitle),
      content: SizedBox(
        width: 400,
        child: _error != null
            ? InlineError(message: _error!)
            : Row(
                children: <Widget>[
                  const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)),
                  const SizedBox(width: AppSpacing.md),
                  Expanded(child: Text(t.accountImportWorking, style: TextStyle(color: palette.textSoft, height: 1.45))),
                ],
              ),
      ),
      actions: <Widget>[if (_error != null) FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.close))],
    );
  }
}

/// What was added, what was there already, and what failed with the reason.
class _ImportResultDialog extends StatelessWidget {
  const _ImportResultDialog({required this.result});

  final Map<String, dynamic> result;

  List<Map> _list(String key) => ((result[key] as List?) ?? const <Object>[]).whereType<Map>().toList();

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final text = TextStyle(color: palette.textSoft, height: 1.45);
    final imported = _list('imported');
    final skipped = _list('skipped');
    final failed = _list('failed');
    String names(List<Map> items) => items.map((item) => '${item['label']}').join(', ');
    return AlertDialog(
      title: Text(t.accountImportDone),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            if (imported.isNotEmpty) InlineSuccess(message: t.accountImportImported(names(imported))) else Text(t.accountImportNothing, style: text),
            if (skipped.isNotEmpty) ...<Widget>[const SizedBox(height: AppSpacing.sm), Text(t.accountImportSkipped(names(skipped)), style: text)],
            if (failed.isNotEmpty) ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              Text(t.accountImportFailed, style: text),
              for (final item in failed) Padding(padding: const EdgeInsets.only(top: 4), child: InlineError(message: '${item['label']}: ${item['message']}')),
            ],
          ],
        ),
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.close))],
    );
  }
}
