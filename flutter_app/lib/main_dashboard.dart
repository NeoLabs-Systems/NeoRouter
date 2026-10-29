import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// Home: is everything working, what happened this week, what to do next.
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
  Map<String, dynamic> _totals = <String, dynamic>{};
  Map<String, dynamic>? _capacity;
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
      final results = await Future.wait(<Future<Object>>[api.accounts(), api.keys(), api.logs(limit: 5), api.summary('7d'), api.capacity()]);
      setState(() {
        _accounts = results[0] as List<ProviderAccount>;
        _keys = (results[1] as List<RouterKey>).where((key) => !key.revoked).toList();
        _logs = results[2] as List<RequestLog>;
        _totals = Map<String, dynamic>.from((results[3] as Map<String, dynamic>)['totals'] as Map? ?? const <String, dynamic>{});
        _capacity = results[4] as Map<String, dynamic>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  String _greeting(AppL10n t) {
    final name = widget.controller.user?.displayName ?? '';
    final hour = DateTime.now().hour;
    if (hour < 12) return t.greetMorning(name);
    if (hour < 18) return t.greetAfternoon(name);
    return t.greetEvening(name);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final active = _accounts.where((account) => account.enabled).toList();
    final problems = active.where((account) => account.status == 'invalid' || account.status == 'error').toList();
    final working = active.where((account) => account.status == 'healthy').length;
    final setupDone = _accounts.isNotEmpty && _keys.isNotEmpty && _logs.isNotEmpty;
    final status = _loading
        ? ''
        : active.isEmpty
            ? t.homeStatusEmpty
            : problems.isEmpty
                ? t.homeStatusOk('$working')
                : t.homeStatusIssues('${problems.length}');
    return PageScaffold(
      header: ScreenHeader(
        title: _greeting(t),
        description: status,
        trailing: setupDone ? FilledButton(onPressed: () => controller.go(AppPage.playground), child: Text(t.homeOpenPlayground)) : null,
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
                _ProblemNotice(account: account, onFix: () => controller.go(AppPage.providers)),
                const SizedBox(height: AppSpacing.sm),
              ],
              if (problems.isNotEmpty) const SizedBox(height: AppSpacing.sm),
              if (!setupDone) ...<Widget>[
                _SetupChecklist(controller: controller, providers: _accounts.length, keys: _keys.length, requests: _logs.length),
                const SizedBox(height: 28),
              ],
              if (_logs.isNotEmpty) ...<Widget>[
                SettingsHeading(title: t.homeLast7Days),
                _WeekStrip(totals: _totals),
                const SizedBox(height: 28),
              ],
              if (active.isNotEmpty && _capacity != null) ...<Widget>[
                _CapacityCard(capacity: _capacity!),
                const SizedBox(height: 28),
              ],
              LayoutBuilder(
                builder: (context, constraints) {
                  final connections = _ConnectionsList(accounts: active, onAll: () => controller.go(AppPage.providers));
                  final recent = _RecentList(logs: _logs, onAll: () => controller.go(AppPage.activity));
                  if (constraints.maxWidth < 860) {
                    return Column(children: <Widget>[recent, const SizedBox(height: 28), connections]);
                  }
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(flex: 6, child: recent),
                      const SizedBox(width: AppSpacing.lg),
                      Expanded(flex: 4, child: connections),
                    ],
                  );
                },
              ),
              const SizedBox(height: 28),
              _UseInApps(endpoint: controller.endpoint, onKeys: () => controller.go(AppPage.keys)),
            ],
          ),
        ),
      ],
    );
  }
}

class _ProblemNotice extends StatelessWidget {
  const _ProblemNotice({required this.account, required this.onFix});

  final ProviderAccount account;
  final VoidCallback onFix;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return AppCard(
      padding: const EdgeInsets.fromLTRB(18, 14, 14, 14),
      child: Row(
        children: <Widget>[
          const ToneDot(tone: PillTone.error, size: 8),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(t.problemTitle(account.label), style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                const SizedBox(height: 2),
                Text(
                  account.status == 'invalid' ? t.problemKeyRejected : t.homeProblemHint,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13, color: palette.textSoft),
                ),
              ],
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          OutlinedButton(onPressed: onFix, child: Text(t.homeFix)),
        ],
      ),
    );
  }
}

class _SetupChecklist extends StatelessWidget {
  const _SetupChecklist({required this.controller, required this.providers, required this.keys, required this.requests});

  final NeoRouterController controller;
  final int providers;
  final int keys;
  final int requests;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final steps = <({String title, String body, bool done, AppPage page})>[
      (title: t.setupStep1Title, body: t.setupStep1Body, done: providers > 0, page: AppPage.providers),
      (title: t.setupStep2Title, body: t.setupStep2Body, done: keys > 0, page: AppPage.keys),
      (title: t.setupStep3Title, body: t.setupStep3Body, done: requests > 0, page: AppPage.playground),
    ];
    final current = steps.indexWhere((step) => !step.done);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.setupTitle, description: t.setupProgress('${steps.where((step) => step.done).length}', '${steps.length}')),
        SettingsCardList(
          children: <Widget>[
            for (var i = 0; i < steps.length; i++)
              _SetupStep(
                number: i + 1,
                title: steps[i].title,
                body: steps[i].body,
                done: steps[i].done,
                current: i == current,
                onStart: () => controller.go(steps[i].page),
              ),
          ],
        ),
      ],
    );
  }
}

class _SetupStep extends StatelessWidget {
  const _SetupStep({required this.number, required this.title, required this.body, required this.done, required this.current, required this.onStart});

  final int number;
  final String title;
  final String body;
  final bool done;
  final bool current;
  final VoidCallback onStart;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
      child: Row(
        children: <Widget>[
          Container(
            width: 28,
            height: 28,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: done ? palette.surfaceRaised : Colors.transparent,
              border: Border.all(color: done ? Colors.transparent : (current ? palette.text : palette.borderStrong)),
            ),
            child: done
                ? Icon(Icons.check_rounded, size: 16, color: palette.textSoft)
                : Text('$number', style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: current ? palette.text : palette.textMuted)),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  title,
                  style: TextStyle(
                    fontWeight: FontWeight.w600,
                    color: done ? palette.textMuted : palette.text,
                    decoration: done ? TextDecoration.lineThrough : null,
                    decorationColor: palette.textMuted,
                  ),
                ),
                if (!done) ...<Widget>[
                  const SizedBox(height: 2),
                  Text(body, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.4)),
                ],
              ],
            ),
          ),
          if (current) ...<Widget>[const SizedBox(width: 12), FilledButton(onPressed: onStart, child: Text(t.setupStart))],
        ],
      ),
    );
  }
}

class _WeekStrip extends StatelessWidget {
  const _WeekStrip({required this.totals});

  final Map<String, dynamic> totals;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final requests = (totals['requests'] as num?)?.toInt() ?? 0;
    final rate = (totals['successRate'] as num?)?.toDouble();
    final cost = (totals['costUsd'] as num?)?.toDouble() ?? 0;
    final free = (totals['freeRequests'] as num?)?.toInt() ?? 0;
    final latency = (totals['avgLatencyMs'] as num?)?.toDouble();
    final saved = (totals['savedUsd'] as num?)?.toDouble() ?? 0;
    return MetricStrip(
      metrics: <({String label, String value, String? caption})>[
        (label: t.metricRequests, value: formatCount(requests), caption: null),
        (label: t.metricAnswered, value: rate == null ? '—' : '${(rate * 100).round()} %', caption: null),
        (label: t.metricSaved, value: formatUsd(saved), caption: t.metricSavedCaption(formatUsd(cost), '${requests == 0 ? 0 : (free * 100 / requests).round()}')),
        (label: t.metricResponse, value: formatMs(latency), caption: null),
      ],
    );
  }
}

/// How many requests the free models could take a day, used non-stop.
class _CapacityCard extends StatelessWidget {
  const _CapacityCard({required this.capacity});

  final Map<String, dynamic> capacity;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final perDay = (capacity['perDay'] as num?)?.toInt() ?? 0;
    final perHour = (capacity['perHour'] as num?)?.toInt() ?? 0;
    final providers = (capacity['providersCounted'] as num?)?.toInt() ?? 0;
    final uncounted = (capacity['modelsWithoutLimit'] as num?)?.toInt() ?? 0;
    return AppCard(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Container(
            width: 40,
            height: 40,
            decoration: BoxDecoration(color: palette.accentSoft, borderRadius: BorderRadius.circular(12)),
            child: Icon(Icons.all_inclusive_rounded, size: 20, color: palette.accent),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(t.homeCapacityTitle.toUpperCase(), style: sectionEyebrowStyle(palette)),
                const SizedBox(height: 6),
                if (perDay == 0)
                  Text(t.homeCapacityNone, style: TextStyle(color: palette.textSoft, height: 1.45))
                else ...<Widget>[
                  Text(t.homeCapacityValue(formatCount(perDay)), style: displayTitleStyle(palette, size: 24)),
                  const SizedBox(height: 6),
                  Text(t.homeCapacityCaption(formatCount(perHour), '$providers'), style: TextStyle(color: palette.textSoft, height: 1.45)),
                ],
                if (uncounted > 0) ...<Widget>[
                  const SizedBox(height: 4),
                  Text(t.homeCapacityUnknown('$uncounted'), style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ConnectionsList extends StatelessWidget {
  const _ConnectionsList({required this.accounts, required this.onAll});

  final List<ProviderAccount> accounts;
  final VoidCallback onAll;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.navProviders, trailing: TextButton(onPressed: onAll, child: Text(t.homeSeeAll))),
        if (accounts.isEmpty)
          AppCard(child: Text(t.noProvidersMessage, style: TextStyle(color: neoRouterPaletteOf(context).textSoft)))
        else
          SettingsCardList(
            children: <Widget>[
              for (final account in accounts.take(6))
                ListRow(
                  leading: ProviderAvatar(label: account.label, size: 30),
                  title: account.label,
                  subtitle: t.homeModelsLine('${account.modelCount}', '${account.freeModelCount}'),
                  trailing: accountStatusDot(account),
                  padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
                ),
            ],
          ),
      ],
    );
  }
}

/// A provider's state as one dot with a tooltip.
Widget accountStatusDot(ProviderAccount account) => Builder(builder: (context) {
      final t = AppL10n.of(context);
      final (label, tone) = accountStatus(t, account);
      return Tooltip(message: label, child: ToneDot(tone: tone));
    });

(String, PillTone) accountStatus(AppL10n t, ProviderAccount account) {
  if (!account.enabled) return (t.statusDisabled, PillTone.neutral);
  return switch (account.status) {
    'healthy' => (t.statusHealthy, PillTone.success),
    'invalid' => (t.statusInvalid, PillTone.error),
    'error' => (t.statusError, PillTone.warning),
    _ => (t.statusUnknown, PillTone.neutral),
  };
}

class _RecentList extends StatelessWidget {
  const _RecentList({required this.logs, required this.onAll});

  final List<RequestLog> logs;
  final VoidCallback onAll;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.homeRecentActivity, trailing: TextButton(onPressed: onAll, child: Text(t.homeSeeAll))),
        if (logs.isEmpty)
          AppCard(child: Text(t.homeNoRequests, style: TextStyle(color: palette.textSoft)))
        else
          SettingsCardList(children: <Widget>[for (final log in logs) RequestRow(log: log)]),
      ],
    );
  }
}

/// One request in plain words: which model answered, when, from which key.
class RequestRow extends StatelessWidget {
  const RequestRow({super.key, required this.log, this.onTap});

  final RequestLog log;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final model = log.model == null ? log.requestedModel : shortModelName(log.model!);
    final subtitle = <String>[
      formatAgo(t, log.createdAt),
      if (log.keyName != null) log.keyName!,
      if (log.success && !log.isFree) formatUsd(log.costUsd),
    ].join(' · ');
    return ListRow(
      onTap: onTap,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
      leading: Icon(
        log.success ? Icons.check_circle_outline_rounded : Icons.error_outline_rounded,
        size: 18,
        color: log.success ? palette.textMuted : palette.error,
      ),
      title: log.success ? model : t.requestFailed(log.errorCode ?? '${log.httpStatus ?? ''}'),
      subtitle: subtitle,
      trailing: Text(formatMs(log.latencyMs), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
    );
  }
}

class _UseInApps extends StatefulWidget {
  const _UseInApps({required this.endpoint, required this.onKeys});

  final String? endpoint;
  final VoidCallback onKeys;

  @override
  State<_UseInApps> createState() => _UseInAppsState();
}

class _UseInAppsState extends State<_UseInApps> {
  bool _showCode = false;
  int _tab = 0;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final endpoint = widget.endpoint ?? 'http://127.0.0.1:4600/v1';
    final base = endpoint.replaceFirst(RegExp(r'/v1$'), '');
    final snippets = <(String, String)>[
      ('Python', 'from openai import OpenAI\n\nclient = OpenAI(base_url="$endpoint", api_key="nrt_…")\nclient.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Hi"}])'),
      ('curl', 'curl $endpoint/chat/completions \\\n  -H "Authorization: Bearer nrt_…" -H "Content-Type: application/json" \\\n  -d \'{"model":"auto","messages":[{"role":"user","content":"Hi"}]}\''),
      ('Claude Code', 'ANTHROPIC_BASE_URL=$base ANTHROPIC_AUTH_TOKEN=nrt_… claude'),
    ];
    void copy(String value) {
      Clipboard.setData(ClipboardData(text: value));
      showSnack(context, t.copied);
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.homeUseTitle, description: t.homeUseBody),
        AppCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Expanded(
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
                      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
                      child: SelectableText(endpoint, maxLines: 1, style: GoogleFonts.geistMono(fontSize: 13.5, color: palette.text)),
                    ),
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton.icon(onPressed: () => copy(endpoint), icon: const Icon(Icons.copy_rounded, size: 15), label: Text(t.copy)),
                ],
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 4,
                children: <Widget>[
                  TextButton(onPressed: widget.onKeys, child: Text(t.homeGetKey)),
                  TextButton.icon(
                    onPressed: () => setState(() => _showCode = !_showCode),
                    icon: Icon(_showCode ? Icons.expand_less_rounded : Icons.code_rounded, size: 16),
                    label: Text(_showCode ? t.homeHideExamples : t.homeShowExamples),
                  ),
                ],
              ),
              if (_showCode) ...<Widget>[
                const SizedBox(height: 8),
                PageTabs<int>(
                  tabs: <({int value, String label, int? count})>[
                    for (var i = 0; i < snippets.length; i++) (value: i, label: snippets[i].$1, count: null),
                  ],
                  selected: _tab,
                  onChanged: (value) => setState(() => _tab = value),
                ),
                const SizedBox(height: 12),
                Container(
                  padding: const EdgeInsets.fromLTRB(16, 12, 4, 12),
                  decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(child: SelectableText(snippets[_tab].$2, style: GoogleFonts.geistMono(fontSize: 12.5, height: 1.6, color: palette.textSoft))),
                      IconButton(tooltip: t.copy, icon: const Icon(Icons.copy_rounded, size: 16), onPressed: () => copy(snippets[_tab].$2)),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}
