import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

class LogsScreen extends StatefulWidget {
  const LogsScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<LogsScreen> createState() => _LogsScreenState();
}

class _LogsScreenState extends State<LogsScreen> {
  List<RequestLog> _logs = <RequestLog>[];
  bool _loading = true;
  String? _error;
  String? _status;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final logs = await widget.controller.api.logs(limit: 200, status: _status);
      setState(() => _logs = logs);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.logsTitle,
        description: t.logsDescription,
        trailing: IconButton(tooltip: t.refresh, onPressed: _load, icon: const Icon(Icons.refresh)),
      ),
      children: <Widget>[
        Wrap(
          spacing: 6,
          children: <Widget>[
            for (final (value, label) in <(String?, String)>[(null, t.filterAll), ('success', t.filterSuccess), ('error', t.filterErrors)])
              ChoiceChip(
                label: Text(label),
                selected: _status == value,
                onSelected: (_) {
                  setState(() => _status = value);
                  _load();
                },
              ),
          ],
        ),
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _logs.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.receipt_long_outlined, title: t.noLogsTitle, message: t.noLogsMessage))
              : SectionCard(
                  padding: const EdgeInsets.symmetric(vertical: AppSpacing.xs),
                  child: Column(
                    children: <Widget>[
                      for (var index = 0; index < _logs.length; index++) ...<Widget>[
                        if (index > 0) Divider(height: 1, color: neoRouterPaletteOf(context).border),
                        _LogRow(log: _logs[index]),
                      ],
                    ],
                  ),
                ),
        ),
      ],
    );
  }
}

class _LogRow extends StatelessWidget {
  const _LogRow({required this.log});

  final RequestLog log;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ExpansionTile(
      tilePadding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
      shape: const Border(),
      leading: Icon(log.success ? Icons.check_circle_outline : Icons.error_outline, color: log.success ? palette.success : palette.error, size: 20),
      title: Text(log.model ?? log.requestedModel, overflow: TextOverflow.ellipsis, style: const TextStyle(fontFamily: 'monospace', fontSize: 13, fontWeight: FontWeight.w600)),
      subtitle: Text(
        [
          formatTime(log.createdAt),
          if (log.keyName != null) log.keyName!,
          log.apiFormat,
          if (log.success) t.tokensInOut(formatCount(log.inputTokens), formatCount(log.outputTokens)) else '${log.httpStatus ?? ''} ${log.errorCode ?? ''}',
          formatMs(log.latencyMs),
          if (log.isFree) t.free else formatUsd(log.costUsd),
          if (log.attempts > 1) t.attempts('${log.attempts}'),
        ].join(' · '),
        style: TextStyle(color: palette.textMuted, fontSize: 12),
      ),
      childrenPadding: const EdgeInsets.fromLTRB(AppSpacing.md, 0, AppSpacing.md, AppSpacing.md),
      expandedCrossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        if (log.errorMessage != null) ...<Widget>[
          Text(log.errorMessage!, style: TextStyle(color: palette.error, fontSize: 12)),
          const SizedBox(height: AppSpacing.sm),
        ],
        Text('requested: ${log.requestedModel}${log.stream ? ' · stream' : ''}${log.ttftMs == null ? '' : ' · ${t.ttft(formatMs(log.ttftMs))}'}', style: TextStyle(color: palette.textSoft, fontSize: 12)),
        const SizedBox(height: AppSpacing.sm),
        Text(t.fallbackTrail.toUpperCase(), style: sectionEyebrowStyle(palette)),
        const SizedBox(height: 6),
        for (final step in log.trail)
          Padding(
            padding: const EdgeInsets.only(bottom: 4),
            child: Row(
              children: <Widget>[
                Icon(step['outcome'] == 'success' ? Icons.arrow_right_alt : Icons.subdirectory_arrow_right, size: 16, color: step['outcome'] == 'success' ? palette.success : palette.warning),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    '${step['model']} · ${step['outcome']}${step['ms'] == null ? '' : ' · ${formatMs(step['ms'] as num)}'}${step['message'] == null ? '' : ' · ${step['message']}'}',
                    style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}
