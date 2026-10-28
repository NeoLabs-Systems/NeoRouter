import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// Home: what needs attention, how to connect an app, provider health with
/// each provider's own quota window, and the latest requests. Charts live on
/// their own page.
class DashboardScreen extends StatefulWidget {
  const DashboardScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  List<ProviderAccount> _accounts = <ProviderAccount>[];
  List<RouterKey> _keys = <RouterKey>[];
  List<RequestLog> _logs = <RequestLog>[];
  Map<String, Map<String, dynamic>> _quotas = <String, Map<String, dynamic>>{};
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
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.accounts(), api.keys(), api.logs(limit: 6), api.quotas()]);
      setState(() {
        _accounts = results[0] as List<ProviderAccount>;
        _keys = (results[1] as List<RouterKey>).where((key) => !key.revoked).toList();
        _logs = results[2] as List<RequestLog>;
        _quotas = <String, Map<String, dynamic>>{
          for (final group in results[3] as List<Map<String, dynamic>>) (group['account'] as Map)['id'] as String: group,
        };
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final active = _accounts.where((account) => account.enabled).toList();
    final problems = active.where((account) => account.status == 'invalid' || account.status == 'error').toList();
    final healthy = active.where((account) => account.status == 'healthy').length;
    final setupDone = _accounts.isNotEmpty && _keys.isNotEmpty;
    return PageScaffold(
      header: ScreenHeader(
        title: _loading || !setupDone ? t.dashboardTitle : (problems.isEmpty ? t.homeAllGood : t.homeNeedsAttention),
        description: _loading || active.isEmpty ? t.dashboardDescription : t.homeHealthyOf('$healthy', '${active.length}'),
        trailing: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: <Widget>[
            OutlinedButton(onPressed: () => controller.go(AppPage.playground), child: Text(t.homeOpenPlayground)),
            FilledButton.icon(
              onPressed: () => controller.go(AppPage.providers),
              icon: const Icon(Icons.add, size: 18),
              label: Text(t.addProvider),
            ),
          ],
        ),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              for (final account in problems) ...<Widget>[
                _ProblemBanner(account: account, onFix: () => controller.go(AppPage.providers)),
                const SizedBox(height: AppSpacing.md),
              ],
              if (!setupDone) ...<Widget>[
                _QuickStart(controller: controller, providers: _accounts.length, keys: _keys.length),
                const SizedBox(height: AppSpacing.lg),
              ],
              _ConnectCard(endpoint: controller.endpoint, sampleKey: _keys.isEmpty ? null : _keys.first),
              const SizedBox(height: AppSpacing.lg),
              LayoutBuilder(
                builder: (context, constraints) {
                  final providers = _ProvidersCard(accounts: active, quotas: _quotas, onManage: () => controller.go(AppPage.providers));
                  final recent = _RecentCard(logs: _logs, onAll: () => controller.go(AppPage.logs));
                  if (constraints.maxWidth < 900) {
                    return Column(children: <Widget>[providers, const SizedBox(height: AppSpacing.lg), recent]);
                  }
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(flex: 2, child: providers),
                      const SizedBox(width: AppSpacing.lg),
                      Expanded(flex: 3, child: recent),
                    ],
                  );
                },
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ProblemBanner extends StatelessWidget {
  const _ProblemBanner({required this.account, required this.onFix});

  final ProviderAccount account;
  final VoidCallback onFix;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final title = '${account.label}: ${account.status == 'invalid' ? t.statusInvalid : t.statusError}';
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 12, 12, 12),
      decoration: BoxDecoration(
        color: palette.error.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(AppRadius.card),
        border: Border.all(color: palette.error.withValues(alpha: 0.3)),
      ),
      child: Row(
        children: <Widget>[
          Icon(Icons.error_outline, size: 20, color: palette.error),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: TextStyle(fontWeight: FontWeight.w700, color: palette.text)),
                const SizedBox(height: 2),
                Text(account.statusMessage ?? t.homeProblemHint, style: TextStyle(fontSize: 13, color: palette.textSoft)),
              ],
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          OutlinedButton(
            onPressed: onFix,
            style: OutlinedButton.styleFrom(foregroundColor: palette.error, side: BorderSide(color: palette.error.withValues(alpha: 0.4))),
            child: Text(t.homeFix),
          ),
        ],
      ),
    );
  }
}

class _ConnectCard extends StatefulWidget {
  const _ConnectCard({required this.endpoint, required this.sampleKey});

  final String? endpoint;
  final RouterKey? sampleKey;

  @override
  State<_ConnectCard> createState() => _ConnectCardState();
}

class _ConnectCardState extends State<_ConnectCard> {
  int _tab = 0;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final endpoint = widget.endpoint ?? 'http://127.0.0.1:4600/v1';
    final base = endpoint.replaceFirst(RegExp(r'/v1$'), '');
    final snippets = <(String, String)>[
      ('curl', 'curl $endpoint/chat/completions \\\n  -H "Authorization: Bearer nrt_…" -H "Content-Type: application/json" \\\n  -d \'{"model":"auto:fast","messages":[{"role":"user","content":"Hi"}]}\''),
      ('Python', 'from openai import OpenAI\nclient = OpenAI(base_url="$endpoint", api_key="nrt_…")\nclient.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hi"}])'),
      ('Claude Code', 'ANTHROPIC_BASE_URL=$base ANTHROPIC_AUTH_TOKEN=nrt_… claude'),
    ];
    final code = snippets[_tab].$2;
    void copy(String value) {
      Clipboard.setData(ClipboardData(text: value));
      showSnack(context, t.copied);
    }

    return SectionCard(
      title: t.homeConnectTitle,
      trailing: SegmentedButton<int>(
        segments: <ButtonSegment<int>>[
          for (var i = 0; i < snippets.length; i++) ButtonSegment<int>(value: i, label: Text(snippets[i].$1)),
        ],
        selected: <int>{_tab},
        showSelectedIcon: false,
        onSelectionChanged: (value) => setState(() => _tab = value.first),
        style: SegmentedButton.styleFrom(
          visualDensity: VisualDensity.compact,
          selectedBackgroundColor: palette.accentSoft,
          selectedForegroundColor: palette.text,
          foregroundColor: palette.textSoft,
          side: BorderSide(color: palette.borderStrong),
          textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(t.homeConnectHint, style: TextStyle(color: palette.textSoft, fontSize: 13)),
          const SizedBox(height: AppSpacing.md),
          Container(
            padding: const EdgeInsets.fromLTRB(16, 6, 6, 6),
            decoration: BoxDecoration(
              color: palette.surfaceRaised,
              borderRadius: BorderRadius.circular(AppRadius.input),
              border: Border.all(color: palette.borderStrong),
            ),
            child: Row(
              children: <Widget>[
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(t.baseUrl, style: TextStyle(color: palette.textMuted, fontSize: 12, fontWeight: FontWeight.w600)),
                      SelectableText(endpoint, style: GoogleFonts.geistMono(fontSize: 13.5, color: palette.text)),
                    ],
                  ),
                ),
                OutlinedButton(onPressed: () => copy(endpoint), child: Text(t.copy)),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Container(
            padding: const EdgeInsets.fromLTRB(16, 12, 6, 12),
            decoration: BoxDecoration(color: palette.scaffold, borderRadius: BorderRadius.circular(AppRadius.tag), border: Border.all(color: palette.border)),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Expanded(child: SelectableText(code, style: GoogleFonts.geistMono(fontSize: 12.5, height: 1.6, color: palette.textSoft))),
                IconButton(tooltip: t.copy, icon: const Icon(Icons.copy_rounded, size: 16), onPressed: () => copy(code)),
              ],
            ),
          ),
          if (widget.sampleKey != null) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            Text(t.homeKeyHint(widget.sampleKey!.name, widget.sampleKey!.prefix), style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
          ],
        ],
      ),
    );
  }
}

class _ProvidersCard extends StatelessWidget {
  const _ProvidersCard({required this.accounts, required this.quotas, required this.onManage});

  final List<ProviderAccount> accounts;
  final Map<String, Map<String, dynamic>> quotas;
  final VoidCallback onManage;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return SectionCard(
      title: t.navProviders,
      trailing: TextButton(onPressed: onManage, child: Text(t.homeManage)),
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg, vertical: AppSpacing.xs),
      child: accounts.isEmpty
          ? Padding(padding: const EdgeInsets.symmetric(vertical: AppSpacing.md), child: Text('—', style: TextStyle(color: neoRouterPaletteOf(context).textMuted)))
          : Column(
              children: <Widget>[
                for (var i = 0; i < accounts.length; i++) ...<Widget>[
                  if (i > 0) Divider(height: 1, color: neoRouterPaletteOf(context).border),
                  _ProviderRow(account: accounts[i], quota: quotas[accounts[i].id]),
                ],
              ],
            ),
    );
  }
}

/// One provider: status and its own tightest quota window with that window's
/// reset time, since every provider resets on its own schedule.
class _ProviderRow extends StatelessWidget {
  const _ProviderRow({required this.account, required this.quota});

  final ProviderAccount account;
  final Map<String, dynamic>? quota;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (statusLabel, color) = switch (account.status) {
      'healthy' => (t.statusHealthy, palette.success),
      'invalid' => (t.statusInvalid, palette.error),
      'error' => (t.statusError, palette.warning),
      _ => (t.statusUnknown, palette.textMuted),
    };
    final window = _tightestWindow((quota?['pool'] as List?)?.cast<Map>() ?? const <Map>[]);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Container(width: 8, height: 8, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
              const SizedBox(width: 10),
              Expanded(child: Text(account.label, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text))),
              Text(
                window == null ? statusLabel : t.homeQuotaLeft('${(window.left * 100).round()}'),
                style: TextStyle(fontSize: 12.5, color: window == null ? color : palette.textSoft),
              ),
            ],
          ),
          if (window != null) ...<Widget>[
            const SizedBox(height: 8),
            Padding(
              padding: const EdgeInsets.only(left: 18),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(AppRadius.pill),
                      child: LinearProgressIndicator(
                        minHeight: 6,
                        value: window.left,
                        backgroundColor: palette.surfaceRaised,
                        valueColor: AlwaysStoppedAnimation<Color>(window.left < 0.15 ? palette.warning : palette.secondary),
                      ),
                    ),
                  ),
                  if (window.resetAt != null) ...<Widget>[
                    const SizedBox(width: 10),
                    Text(t.homeResetsIn(_untilLabel(window.resetAt!)), style: TextStyle(fontSize: 12, color: palette.textMuted)),
                  ],
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }

  static ({double left, DateTime? resetAt})? _tightestWindow(List<Map> pool) {
    ({double left, DateTime? resetAt})? tightest;
    for (final window in pool) {
      final limit = (window['limit'] as num?)?.toDouble();
      final remaining = (window['remaining'] as num?)?.toDouble();
      if (limit == null || limit <= 0 || remaining == null) continue;
      final left = (remaining / limit).clamp(0, 1).toDouble();
      final resetAt = window['resetAt'] as num?;
      if (tightest == null || left < tightest.left) {
        tightest = (left: left, resetAt: resetAt == null ? null : DateTime.fromMillisecondsSinceEpoch(resetAt.toInt()));
      }
    }
    return tightest;
  }

  static String _untilLabel(DateTime at) {
    final diff = at.difference(DateTime.now());
    if (diff.inSeconds < 60) return '${diff.inSeconds.clamp(0, 59)} s';
    if (diff.inMinutes < 60) return '${diff.inMinutes} min';
    if (diff.inHours < 48) return '${diff.inHours} h';
    return '${diff.inDays} d';
  }
}

class _RecentCard extends StatelessWidget {
  const _RecentCard({required this.logs, required this.onAll});

  final List<RequestLog> logs;
  final VoidCallback onAll;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return SectionCard(
      title: t.homeRecent,
      trailing: TextButton(onPressed: onAll, child: Text(t.homeAllActivity)),
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg, vertical: AppSpacing.xs),
      child: logs.isEmpty
          ? Padding(padding: const EdgeInsets.symmetric(vertical: AppSpacing.md), child: Text(t.homeNoRequests, style: TextStyle(color: palette.textMuted)))
          : Column(
              children: <Widget>[
                for (var i = 0; i < logs.length; i++) ...<Widget>[
                  if (i > 0) Divider(height: 1, color: palette.border),
                  _RecentRow(log: logs[i]),
                ],
              ],
            ),
    );
  }
}

class _RecentRow extends StatelessWidget {
  const _RecentRow({required this.log});

  final RequestLog log;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (label, tone) = !log.success
        ? (log.errorCode ?? '${log.httpStatus ?? '—'}', PillTone.error)
        : log.attempts > 1
            ? (t.homeRetried, PillTone.warning)
            : (t.homeOk, PillTone.success);
    final routed = log.model != null && log.model != log.requestedModel;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Text.rich(
              TextSpan(
                children: <InlineSpan>[
                  TextSpan(
                    text: log.requestedModel,
                    style: GoogleFonts.geistMono(fontSize: 12.5, color: routed ? palette.accentStrong : palette.text),
                  ),
                  if (routed) ...<InlineSpan>[
                    TextSpan(text: '  →  ', style: TextStyle(color: palette.textMuted)),
                    TextSpan(text: log.model, style: TextStyle(fontSize: 13, color: palette.text)),
                  ],
                ],
              ),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          MetaPill(label: label, tone: tone),
          SizedBox(
            width: 72,
            child: Text(formatMs(log.latencyMs), textAlign: TextAlign.right, style: TextStyle(fontSize: 12.5, color: palette.textSoft)),
          ),
          SizedBox(
            width: 64,
            child: Text(
              log.isFree ? t.free : formatUsd(log.costUsd),
              textAlign: TextAlign.right,
              style: TextStyle(fontSize: 12.5, color: palette.textSoft),
            ),
          ),
        ],
      ),
    );
  }
}

class _QuickStart extends StatelessWidget {
  const _QuickStart({required this.controller, required this.providers, required this.keys});

  final NeoRouterController controller;
  final int providers;
  final int keys;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    Widget step(int number, String text, bool done, AppPage page) => ListTile(
          contentPadding: EdgeInsets.zero,
          leading: CircleAvatar(
            radius: 15,
            backgroundColor: done ? palette.success.withValues(alpha: 0.15) : palette.accentSoft,
            child: done
                ? Icon(Icons.check, size: 16, color: palette.success)
                : Text('$number', style: TextStyle(color: palette.accentStrong, fontWeight: FontWeight.w700, fontSize: 13)),
          ),
          title: Text(text, style: done ? TextStyle(color: palette.textMuted, decoration: TextDecoration.lineThrough) : null),
          trailing: done ? null : const Icon(Icons.arrow_forward, size: 18),
          onTap: done ? null : () => controller.go(page),
        );
    return SectionCard(
      title: t.quickStartTitle,
      child: Column(
        children: <Widget>[
          step(1, t.quickStartProviders, providers > 0, AppPage.providers),
          step(2, t.quickStartKeys, keys > 0, AppPage.keys),
          step(3, t.quickStartUse, false, AppPage.playground),
        ],
      ),
    );
  }
}
