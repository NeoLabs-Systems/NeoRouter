import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_slot_plans.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// Edits the limits for one provider connection, even when opened from a
/// provider with several connections.
class ProviderTrafficLimitsDialog extends StatefulWidget {
  const ProviderTrafficLimitsDialog({
    super.key,
    required this.controller,
    required this.accounts,
  });

  final NeoRouterController controller;
  final List<ProviderAccount> accounts;

  @override
  State<ProviderTrafficLimitsDialog> createState() =>
      _ProviderTrafficLimitsDialogState();
}

class _ProviderTrafficLimitsDialogState
    extends State<ProviderTrafficLimitsDialog> {
  static const _metrics = ['concurrent', 'rpm', 'rpd', 'tpm', 'tpd'];

  ProviderAccount? _account;
  final _fields = {
    for (final metric in _metrics) metric: TextEditingController(),
  };
  final _wait = TextEditingController();
  String _overflow = 'auto';
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    if (widget.accounts.length == 1) _select(widget.accounts.single);
  }

  void _select(ProviderAccount account) {
    _account = account;
    final previous = account.raw['limits'] as Map?;
    for (final entry in _fields.entries) {
      entry.value.text = previous?[entry.key]?.toString() ?? '';
    }
    _overflow = account.raw['overflowPolicy'] as String? ?? 'auto';
    final ms = account.raw['slotWaitMs'] as num?;
    _wait.text = ms == null ? '' : (ms / 1000).toString();
    _error = null;
  }

  @override
  void dispose() {
    for (final field in _fields.values) {
      field.dispose();
    }
    _wait.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppL10n.of(context);
    final account = _account!;
    final previous = account.raw['limits'] as Map?;
    final limits = <String, int?>{};
    for (final entry in _fields.entries) {
      final text = entry.value.text.trim();
      final value = int.tryParse(text);
      if (text.isNotEmpty &&
          (value == null || value < 1 || value > 10000000000)) {
        setState(() => _error = t.limitsInvalid);
        return;
      }
      if (value != previous?[entry.key]) limits[entry.key] = value;
    }
    final body = <String, Object?>{if (limits.isNotEmpty) 'limits': limits};
    if (account.billingMode == 'flat_rate') {
      final text = _wait.text.trim();
      final seconds = double.tryParse(text);
      final maxWaitMs = account.raw['slotMaxWaitMs'] as num?;
      if (text.isNotEmpty &&
          (seconds == null ||
              !seconds.isFinite ||
              seconds < 0 ||
              (maxWaitMs != null && seconds * 1000 > maxWaitMs))) {
        setState(() => _error = t.limitsInvalid);
        return;
      }
      final waitMs = seconds == null ? null : (seconds * 1000).round();
      if (waitMs != account.raw['slotWaitMs']) body['slotWaitMs'] = waitMs;
      if (_overflow != account.raw['overflowPolicy']) {
        body['overflowPolicy'] = _overflow;
      }
    }
    if (body.isEmpty) {
      Navigator.of(context).pop();
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.controller.api.updateAccount(account.id, body);
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    } catch (_) {
      if (mounted) setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Widget _field(AppL10n t, String metric, String label) => TextField(
    controller: _fields[metric],
    enabled: !_saving,
    keyboardType: TextInputType.number,
    decoration: InputDecoration(labelText: label, hintText: t.limitAutomatic),
  );

  Widget _pair(Widget first, Widget second) => LayoutBuilder(
    builder: (context, constraints) => constraints.maxWidth < 390
        ? Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              first,
              const SizedBox(height: AppSpacing.sm),
              second,
            ],
          )
        : Row(
            children: [
              Expanded(child: first),
              const SizedBox(width: AppSpacing.sm),
              Expanded(child: second),
            ],
          ),
  );

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final account = _account;
    final provider = widget.accounts.first.providerName;
    return PopScope(
      canPop: !_saving,
      child: DialogFrame(
        maxWidth: 560,
        leading: ProviderAvatar(label: provider, size: 40),
        title: t.trafficLimitsTitle,
        subtitle: Text(
          account?.label ?? provider,
          style: TextStyle(color: palette.textSoft),
        ),
        body: account == null
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(t.providerLimitsChooseConnection),
                  const SizedBox(height: AppSpacing.sm),
                  SettingsCardList(
                    children: [
                      for (final item in widget.accounts)
                        ListTile(
                          title: Text(item.label),
                          subtitle: item.label == item.slug
                              ? null
                              : Text(item.slug),
                          trailing: const Icon(Icons.chevron_right_rounded),
                          onTap: () => setState(() => _select(item)),
                        ),
                    ],
                  ),
                ],
              )
            : Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    t.providerTrafficLimitsHint,
                    style: TextStyle(color: palette.textSoft),
                  ),
                  const SizedBox(height: AppSpacing.lg),
                  _field(t, 'concurrent', t.limitConcurrent),
                  const SizedBox(height: AppSpacing.md),
                  _pair(
                    _field(t, 'rpm', t.metricRpm),
                    _field(t, 'rpd', t.metricRpd),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  _pair(
                    _field(t, 'tpm', t.metricTpm),
                    _field(t, 'tpd', t.metricTpd),
                  ),
                  if (account.billingMode == 'flat_rate') ...[
                    const SizedBox(height: AppSpacing.md),
                    AbsorbPointer(
                      absorbing: _saving,
                      child: SlotOverflowControls(
                        policy: _overflow,
                        onChanged: (value) => setState(() => _overflow = value),
                        wait: _wait,
                      ),
                    ),
                  ],
                  if (_error case final error?) ...[
                    const SizedBox(height: AppSpacing.sm),
                    InlineError(message: error),
                  ],
                ],
              ),
        actions: [
          TextButton(
            onPressed: _saving ? null : () => Navigator.of(context).pop(),
            child: Text(t.cancel),
          ),
          if (account != null)
            FilledButton(
              onPressed: _saving ? null : _save,
              child: _saving
                  ? const SizedBox.square(
                      dimension: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(t.save),
            ),
        ],
      ),
    );
  }
}
