import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_shared.dart';
import 'main_theme.dart';

const keyExpirySoon = Duration(days: 7);
const keyExpiryDurations = <int>[7, 30, 90];

String keyExpiryTime(BuildContext context, DateTime value) {
  final local = value.toLocal();
  final formatted = DateFormat.yMMMd(
    Localizations.localeOf(context).toString(),
  ).add_jm().format(local);
  final offset = local.timeZoneOffset;
  final hours = offset.inHours.abs().toString().padLeft(2, '0');
  final minutes = (offset.inMinutes.abs() % 60).toString().padLeft(2, '0');
  return '$formatted · UTC${offset.isNegative ? '-' : '+'}$hours:$minutes';
}

class KeyExpiryStatus extends StatelessWidget {
  const KeyExpiryStatus({
    super.key,
    required this.expiresAt,
    this.expired = false,
  });
  final DateTime? expiresAt;
  final bool expired;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final deadline = expiresAt;
    final now = DateTime.now();
    final past = deadline != null && (expired || !deadline.isAfter(now));
    final soon =
        deadline != null && !past && deadline.difference(now) <= keyExpirySoon;
    return Wrap(
      spacing: 8,
      runSpacing: 4,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        if (past) MetaPill(label: t.keyExpired, tone: PillTone.error),
        if (soon) MetaPill(label: t.keyExpiringSoon, tone: PillTone.warning),
        Text(
          deadline == null
              ? t.keyExpiryNever
              : past
              ? t.keyExpiredAt(keyExpiryTime(context, deadline))
              : t.keyExpiryAt(keyExpiryTime(context, deadline)),
          style: TextStyle(
            fontSize: 12.5,
            color: past
                ? palette.error
                : soon
                ? palette.warning
                : palette.textMuted,
          ),
        ),
      ],
    );
  }
}

class KeyExpiryField extends StatefulWidget {
  const KeyExpiryField({
    super.key,
    required this.value,
    required this.onChanged,
    this.wasExpired = false,
    this.enabled = true,
  });
  final DateTime? value;
  final ValueChanged<DateTime?> onChanged;
  final bool wasExpired;
  final bool enabled;

  @override
  State<KeyExpiryField> createState() => _KeyExpiryFieldState();
}

class _KeyExpiryFieldState extends State<KeyExpiryField> {
  late String _choice = widget.value == null ? 'never' : 'custom';
  int _dropdownRevision = 0;

  Future<void> _pick() async {
    final now = DateTime.now();
    final initial = widget.value?.toLocal();
    final next = initial != null && initial.isAfter(now)
        ? initial
        : now.add(const Duration(hours: 1));
    final date = await showDatePicker(
      context: context,
      initialDate: DateTime(next.year, next.month, next.day),
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(9999, 12, 31),
    );
    if (date == null || !mounted) return;
    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(next),
    );
    if (time == null || !mounted) return;
    setState(() => _choice = 'custom');
    widget.onChanged(
      DateTime(date.year, date.month, date.day, time.hour, time.minute),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final future =
        widget.value == null || widget.value!.isAfter(DateTime.now());
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        DropdownButtonFormField<String>(
          key: ValueKey('$_choice-$_dropdownRevision'),
          initialValue: _choice,
          isExpanded: true,
          decoration: InputDecoration(labelText: t.keyExpiryTitle),
          items: [
            DropdownMenuItem(value: 'never', child: Text(t.keyExpiryNever)),
            for (final days in keyExpiryDurations)
              DropdownMenuItem(
                value: '$days',
                child: Text(t.keyExpiryDays('$days')),
              ),
            DropdownMenuItem(value: 'custom', child: Text(t.keyExpiryCustom)),
          ],
          onChanged: !widget.enabled
              ? null
              : (value) async {
                  if (value == 'custom') {
                    await _pick();
                    if (mounted) setState(() => _dropdownRevision++);
                    return;
                  }
                  if (value == null) return;
                  setState(() => _choice = value);
                  widget.onChanged(
                    value == 'never'
                        ? null
                        : DateTime.now().add(Duration(days: int.parse(value))),
                  );
                },
        ),
        const SizedBox(height: 6),
        Text(
          t.keyExpiryHint,
          style: TextStyle(fontSize: 12.5, color: palette.textMuted),
        ),
        if (widget.value != null) ...[
          const SizedBox(height: 8),
          KeyExpiryStatus(expiresAt: widget.value),
          if (_choice == 'custom')
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: widget.enabled ? _pick : null,
                icon: const Icon(Icons.event_outlined, size: 17),
                label: Text(t.keyExpiryChange),
              ),
            ),
        ],
        if (widget.wasExpired) ...[
          const SizedBox(height: 6),
          Text(
            future ? t.keyExpiryRenewSave : t.keyExpiryRenewHint,
            style: TextStyle(fontSize: 12.5, color: palette.textSoft),
          ),
        ],
      ],
    );
  }
}
