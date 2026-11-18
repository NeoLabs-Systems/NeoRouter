import 'dart:async';

import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// "2.4 GB" or "350 MB", with the decimal mark of the dashboard's language.
String formatMegabytes(BuildContext context, num megabytes) {
  final text = megabytes >= 1024 ? '${(megabytes / 1024).toStringAsFixed(megabytes >= 10240 ? 0 : 1)} GB' : '${megabytes.round()} MB';
  return Localizations.localeOf(context).languageCode == 'de' ? text.replaceAll('.', ',') : text;
}

String _bytes(BuildContext context, int bytes) => formatMegabytes(context, bytes / (1024 * 1024));

const List<String> _kinds = <String>['chat', 'moderation', 'embedding', 'transcription', 'speech'];

String _kindLabel(AppL10n t, String kind) => switch (kind) {
  'chat' => t.lmKindChat,
  'moderation' => t.lmKindModeration,
  'embedding' => t.lmKindEmbedding,
  'transcription' => t.lmKindTranscription,
  'speech' => t.lmKindSpeech,
  _ => t.lmKindAll,
};

IconData _kindIcon(String kind) => switch (kind) {
  'moderation' => Icons.shield_outlined,
  'embedding' => Icons.scatter_plot_outlined,
  'transcription' => Icons.mic_none_rounded,
  'speech' => Icons.record_voice_over_outlined,
  _ => Icons.chat_bubble_outline_rounded,
};

/// What the privacy filter is doing, in a sentence (also used on the server page).
String privacyFilterState(AppL10n t, Map<String, dynamic> filter) {
  if (filter['enabled'] != true) return t.privacyModelOff;
  return switch ('${filter['status']}') {
    'downloading' => t.privacyModelDownloading('${(((filter['progress'] as num?) ?? 0) * 100).round()}'),
    'loading' => t.privacyModelLoading,
    'ready' => t.privacyModelReady,
    'error' => t.privacyModelError('${filter['error'] ?? ''}'),
    _ => t.privacyModelIdle,
  };
}

/// Admin → Local models: this machine, the models on it and the ones that fit.
class LocalModelsSection extends StatefulWidget {
  const LocalModelsSection({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<LocalModelsSection> createState() => _LocalModelsSectionState();
}

class _LocalModelsSectionState extends State<LocalModelsSection> {
  LocalModelsOverview? _overview;
  String? _error;
  String _kind = 'all';
  bool _showTooBig = false;
  final Set<String> _working = <String>{};
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  // Quick while something downloads or starts, slower otherwise (to notice crashes and repairs).
  void _schedule() {
    _poll?.cancel();
    _poll = Timer(Duration(seconds: _overview?.busy == true ? 2 : 10), _load);
  }

  Future<void> _load() async {
    try {
      final overview = await widget.controller.api.localModels();
      if (!mounted) return;
      setState(() {
        _overview = overview;
        _error = null;
      });
    } on ApiException catch (error) {
      if (mounted && _overview == null) setState(() => _error = error.message);
    } finally {
      if (mounted) _schedule();
    }
  }

  Future<void> _run(String id, Future<LocalModelsOverview> Function() action) async {
    setState(() => _working.add(id));
    try {
      final overview = await action();
      if (mounted) setState(() => _overview = overview);
      _schedule();
    } on ApiException catch (error) {
      if (mounted) showSnack(context, _errorText(AppL10n.of(context), error));
    } finally {
      if (mounted) setState(() => _working.remove(id));
    }
  }

  // The server's reasons, in the dashboard's language.
  String _errorText(AppL10n t, ApiException error) => switch (error.code) {
    'LOCAL_MODEL_TOO_BIG' => t.lmErrTooBig,
    'LOCAL_MODEL_NO_DISK' => t.lmErrNoDisk,
    'LOCAL_MODEL_UNSUPPORTED' => t.lmErrUnsupported,
    'LOCAL_MODEL_INSTALLED' => t.lmErrInstalled,
    'LOCAL_MODEL_NO_STARTER' => t.lmErrNoStarter,
    _ => error.message,
  };

  Future<void> _install(LocalCatalogModel model) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(
      context,
      title: t.lmInstallTitle(model.name),
      message: t.lmInstallConfirm(formatMegabytes(context, model.downloadMb)),
      confirmLabel: t.lmInstall,
      cancelLabel: t.cancel,
      destructive: false,
    );
    if (confirmed) await _run(model.id, () => widget.controller.api.localModelAction(model.id, 'install'));
  }

  Future<void> _remove(LocalModel model) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.lmRemoveTitle(model.name), message: t.lmRemoveConfirm, confirmLabel: t.lmRemove, cancelLabel: t.cancel);
    if (confirmed) await _run(model.id, () => widget.controller.api.removeLocalModel(model.id));
  }

  @override
  Widget build(BuildContext context) {
    final overview = _overview;
    return AsyncBody(
      loading: overview == null && _error == null,
      error: overview == null ? _error : null,
      onRetry: _load,
      child: overview == null ? const SizedBox() : _content(context, overview),
    );
  }

  Widget _content(BuildContext context, LocalModelsOverview overview) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _ServerCard(overview: overview),
        SettingsGroup(
          title: t.lmInstalledTitle,
          children: <Widget>[
            if (overview.installed.isEmpty && overview.starter.isNotEmpty)
              _starterRow(context, overview)
            else if (overview.installed.isEmpty)
              SettingsRow(label: t.lmInstalledEmpty)
            else
              for (final model in overview.installed)
                _InstalledRow(
                  model: model,
                  working: _working.contains(model.id),
                  onToggle: (on) => _run(model.id, () => widget.controller.api.localModelAction(model.id, on ? 'start' : 'stop')),
                  onRetry: () => _run(model.id, () => widget.controller.api.localModelAction(model.id, 'retry')),
                  onRepair: () => _run(model.id, () => widget.controller.api.localModelAction(model.id, 'repair')),
                  onTurnOff: () => _run(model.id, () => widget.controller.api.localModelAction(model.id, 'stop')),
                  onRemove: () => _remove(model),
                ),
          ],
        ),
        // The privacy filter is a local model too, run inside NeoRouter.
        SettingsGroup(
          title: t.privacyModelTitle,
          description: t.privacyModelHint,
          children: <Widget>[
            SettingsRow(
              label: t.privacyModelEnable,
              description: privacyFilterState(t, overview.privacyFilter),
              control: _working.contains('privacy')
                  ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
                  : Switch(
                      value: overview.privacyFilter['enabled'] == true,
                      onChanged: (value) => _run('privacy', () => widget.controller.api.setPrivacyFilter(enabled: value)),
                    ),
            ),
          ],
        ),
        SettingsGroup(
          title: t.lmAccessTitle,
          children: <Widget>[
            SettingsRow(
              label: t.lmAccessEveryone,
              description: t.lmAccessHint,
              control: Switch(
                value: overview.everyoneMayUse,
                onChanged: (value) => _run('access', () => widget.controller.api.setLocalModelsAccess(everyone: value)),
              ),
            ),
          ],
        ),
        _catalog(context, overview),
      ],
    );
  }

  // Nothing installed yet: one button for a set that suits this machine.
  Widget _starterRow(BuildContext context, LocalModelsOverview overview) {
    final t = AppL10n.of(context);
    final starter = overview.starter;
    final size = starter.fold<int>(0, (sum, item) => sum + item.downloadMb);
    return SettingsRow(
      label: t.lmStarterTitle,
      description: t.lmStarterHint(starter.map((item) => item.name).join(' · '), formatMegabytes(context, size)),
      control: _working.contains('starter')
          ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
          : FilledButton(onPressed: () => _run('starter', widget.controller.api.installLocalStarter), child: Text(t.lmStarterInstall)),
    );
  }

  Widget _catalog(BuildContext context, LocalModelsOverview overview) {
    final t = AppL10n.of(context);
    final ofKind = overview.catalog.where((model) => _kind == 'all' || model.kind == _kind).toList()
      ..sort((a, b) {
        if (a.recommended != b.recommended) return a.recommended ? -1 : 1;
        return a.downloadMb.compareTo(b.downloadMb);
      });
    final fitting = ofKind.where((model) => model.runs).toList();
    final tooBig = ofKind.where((model) => !model.runs).toList();
    final shown = <LocalCatalogModel>[...fitting, if (_showTooBig) ...tooBig];
    return Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          SettingsHeading(title: t.lmAddTitle, description: t.lmAddHint),
          if (!overview.textModelsSupported) ...<Widget>[InlineHint(message: t.lmTextUnsupported), const SizedBox(height: 12)],
          PageTabs<String>(
            tabs: <({String value, String label, int? count})>[
              (value: 'all', label: t.lmKindAll, count: null),
              for (final kind in _kinds) (value: kind, label: _kindLabel(t, kind), count: overview.catalog.where((model) => model.kind == kind && model.runs).length),
            ],
            selected: _kind,
            onChanged: (kind) => setState(() => _kind = kind),
          ),
          const SizedBox(height: 14),
          if (shown.isEmpty)
            SettingsCardList(children: <Widget>[SettingsRow(label: t.lmNothingFits)])
          else
            SettingsCardList(
              children: <Widget>[
                for (final model in shown)
                  _CatalogRow(model: model, working: _working.contains(model.id), onInstall: () => _install(model)),
              ],
            ),
          if (tooBig.isNotEmpty)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                onPressed: () => setState(() => _showTooBig = !_showTooBig),
                child: Text(_showTooBig ? t.lmHideTooBig : t.lmShowTooBig('${tooBig.length}')),
              ),
            ),
        ],
      ),
    );
  }
}

/// Processor, memory, graphics and disk in plain words.
class _ServerCard extends StatelessWidget {
  const _ServerCard({required this.overview});

  final LocalModelsOverview overview;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final gpu = overview.gpu;
    final total = overview.memoryMb;
    final others = overview.memoryOthersMb;
    final value = TextStyle(color: palette.textSoft, fontSize: 13);
    return SettingsGroup(
      title: t.lmThisServer,
      description: t.lmIntro,
      children: <Widget>[
        SettingsRow(label: t.lmProcessor, control: Text(t.lmProcessorValue(overview.cpuName, '${overview.cpuCores}'), style: value)),
        // Label and total on one line, the bar across the full width: reads the same on a phone.
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Expanded(child: Text(t.lmMemory, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: palette.text))),
                  Text(formatMegabytes(context, overview.memoryMb), style: value),
                ],
              ),
              const SizedBox(height: 10),
              // The whole memory: what everything else uses on average plus the local models.
              UsageBar(fraction: total == 0 ? 0 : ((others ?? 0) + overview.memoryUsedMb) / total),
              const SizedBox(height: 8),
              Text(
                <String>[
                  t.lmMemoryModels(formatMegabytes(context, overview.memoryUsedMb)),
                  others == null ? t.lmMemoryMeasuring : t.lmMemoryOthers(formatMegabytes(context, others)),
                  t.lmMemoryFree(formatMegabytes(context, (overview.memoryRoomMb - overview.memoryUsedMb).clamp(0, total))),
                ].join(' · '),
                style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45),
              ),
              if (overview.memoryLimited) Text(t.lmMemoryLimited, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
            ],
          ),
        ),
        SettingsRow(
          label: t.lmGraphics,
          control: Text(gpu == null ? t.lmNoGraphics : t.lmGraphicsValue(gpu.name, formatMegabytes(context, gpu.memoryMb)), style: value),
        ),
        if (overview.diskFreeMb != null) SettingsRow(label: t.lmDisk, control: Text(formatMegabytes(context, overview.diskFreeMb!), style: value)),
      ],
    );
  }
}

class _KindAvatar extends StatelessWidget {
  const _KindAvatar({required this.kind});

  final String kind;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      width: 34,
      height: 34,
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Icon(_kindIcon(kind), size: 17, color: palette.textSoft),
    );
  }
}

/// One installed model: what it is doing, an on/off switch, and a ⋯ menu.
class _InstalledRow extends StatelessWidget {
  const _InstalledRow({
    required this.model,
    required this.working,
    required this.onToggle,
    required this.onRetry,
    required this.onRepair,
    required this.onTurnOff,
    required this.onRemove,
  });

  final LocalModel model;
  final bool working;
  final ValueChanged<bool> onToggle;
  final VoidCallback onRetry;
  final VoidCallback onRepair;
  final VoidCallback onTurnOff;
  final VoidCallback onRemove;

  // Not enough memory is fixed by stopping another model, not by checking files.
  bool get _memoryProblem => model.problemCode == 'not_enough_memory';

  String? _timeLeft(AppL10n t) {
    final seconds = model.secondsLeft;
    if (seconds == null) return null;
    if (seconds < 60) return t.lmLeftSeconds;
    final minutes = (seconds / 60).ceil();
    if (minutes < 60) return t.lmLeftMinutes('$minutes');
    return t.lmLeftHours('${minutes ~/ 60}', '${minutes % 60}');
  }

  String _problem(AppL10n t) => switch (model.problemCode) {
    'not_enough_memory' => t.lmProblemMemory,
    'not_enough_disk' => t.lmProblemDisk,
    'crashed' => t.lmProblemCrashed,
    'not_answering' => t.lmProblemNotAnswering,
    'download_failed' => t.lmProblemDownload,
    _ => model.state == 'download_failed' ? t.lmProblemDownload : t.lmProblemStart,
  };

  // Once measured, how much memory it really takes.
  String _withMemory(BuildContext context, String text) =>
      model.memoryMeasured && model.memoryMb != null ? '$text · ${formatMegabytes(context, model.memoryMb!)}' : text;

  String _withTimeLeft(AppL10n t, String text) {
    final left = _timeLeft(t);
    return left == null ? text : '$text · $left';
  }

  (String, PillTone) _status(BuildContext context, AppL10n t) {
    final progress = (_bytes(context, model.doneBytes), _bytes(context, model.totalBytes));
    return switch (model.state) {
      'downloading' => switch (model.phase) {
        'checking' => (t.lmChecking, PillTone.neutral),
        'runtime' => (_withTimeLeft(t, t.lmGettingEngine(progress.$1, progress.$2)), PillTone.neutral),
        'model' => (_withTimeLeft(t, t.lmDownloading(progress.$1, progress.$2)), PillTone.neutral),
        _ => (t.lmPreparing, PillTone.neutral),
      },
      'download_failed' => (t.lmDownloadFailed(_problem(t)), PillTone.error),
      'running' => (_withMemory(context, model.onGpu == true ? t.lmRunningGpu : model.onGpu == false ? t.lmRunningCpu : t.lmRunning), PillTone.success),
      'starting' => (t.lmStarting, PillTone.warning),
      'restarting' => (t.lmRestarting, PillTone.warning),
      'failed' => (t.lmFailed(_problem(t)), PillTone.error),
      _ => (t.lmStopped, PillTone.neutral),
    };
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (status, tone) = _status(context, t);
    final downloading = model.state == 'downloading';
    final fraction = model.totalBytes > 0 ? model.doneBytes / model.totalBytes : null;
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 14, 8, 14),
      child: Row(
        children: <Widget>[
          _KindAvatar(kind: model.kind),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(model.name, style: TextStyle(color: palette.text, fontSize: 14, fontWeight: FontWeight.w600)),
                const SizedBox(height: 4),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Padding(padding: const EdgeInsets.only(top: 5), child: ToneDot(tone: tone)),
                    const SizedBox(width: 7),
                    Expanded(child: Text(status, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.4))),
                  ],
                ),
                if (downloading) ...<Widget>[
                  const SizedBox(height: 8),
                  ClipRRect(
                    borderRadius: BorderRadius.circular(AppRadius.pill),
                    child: LinearProgressIndicator(
                      minHeight: 4,
                      value: model.phase == 'model' || model.phase == 'runtime' ? fraction : null,
                      backgroundColor: palette.surfaceRaised,
                      valueColor: AlwaysStoppedAnimation<Color>(palette.textSoft),
                    ),
                  ),
                ],
              ],
            ),
          ),
          const SizedBox(width: 12),
          if (working)
            const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
          else if (model.state == 'download_failed')
            OutlinedButton(onPressed: onRetry, child: Text(t.retry))
          else if (model.state == 'failed')
            OutlinedButton(onPressed: _memoryProblem ? onRetry : onRepair, child: Text(_memoryProblem ? t.retry : t.lmRepair))
          else if (model.installedFiles)
            Switch(value: model.enabled && model.state != 'stopped', onChanged: onToggle),
          PopupMenuButton<String>(
            icon: Icon(Icons.more_horiz, color: palette.textMuted),
            onSelected: (value) => switch (value) {
              'repair' => onRepair(),
              'off' => onTurnOff(),
              _ => onRemove(),
            },
            itemBuilder: (_) => <PopupMenuEntry<String>>[
              // A failed model shows a button instead of its switch; this turns it off.
              if (model.state == 'failed') PopupMenuItem<String>(value: 'off', child: Text(t.lmTurnOff)),
              if (model.installedFiles) PopupMenuItem<String>(value: 'repair', child: Text(t.lmRepair)),
              PopupMenuItem<String>(value: 'remove', child: Text(downloading ? t.lmCancelDownload : t.lmRemove)),
            ],
          ),
        ],
      ),
    );
  }
}

/// A model that could be installed: what it is good for, how well it runs
/// here and what it needs. Languages and license open on demand.
class _CatalogRow extends StatefulWidget {
  const _CatalogRow({required this.model, required this.working, required this.onInstall});

  final LocalCatalogModel model;
  final bool working;
  final VoidCallback onInstall;

  @override
  State<_CatalogRow> createState() => _CatalogRowState();
}

class _CatalogRowState extends State<_CatalogRow> {
  bool _open = false;

  LocalCatalogModel get model => widget.model;

  (String, PillTone) _rating(AppL10n t) => switch (model.rating) {
    'great' => (t.lmRatingGreat, PillTone.success),
    'good' => (t.lmRatingGood, PillTone.success),
    'slow' => (t.lmRatingSlow, PillTone.warning),
    'tight' => (t.lmRatingTight, PillTone.warning),
    'too_big' => (t.lmRatingTooBig, PillTone.error),
    _ => (t.lmRatingUnsupported, PillTone.error),
  };

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final language = Localizations.localeOf(context).languageCode;
    final (rating, tone) = _rating(t);
    final languages = model.languages == 'many' ? t.lmManyLanguages : ((model.languages as List?) ?? const <Object>[]).map((code) => '$code'.toUpperCase()).join(', ');
    final Widget action;
    if (model.installed) {
      action = Text(t.lmInstalled, style: TextStyle(color: palette.textMuted, fontSize: 13));
    } else if (widget.working) {
      action = const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2));
    } else if (!model.runs) {
      action = const SizedBox();
    } else if (!model.diskOk) {
      action = Text(t.lmNoDisk, style: TextStyle(color: palette.textMuted, fontSize: 13));
    } else {
      action = OutlinedButton(onPressed: widget.onInstall, child: Text(t.lmInstall));
    }
    final details = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Wrap(
          spacing: 8,
          runSpacing: 4,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Text(model.name, style: TextStyle(color: palette.text, fontSize: 14, fontWeight: FontWeight.w600)),
            if (model.recommended) MetaPill(label: t.lmRecommended, tone: PillTone.accent),
          ],
        ),
        const SizedBox(height: 4),
        Text(localizedText(model.summary, language), style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
        if (model.rating == 'tight') ...<Widget>[
          const SizedBox(height: 4),
          Text(t.lmTightHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5, height: 1.45)),
        ],
        const SizedBox(height: 8),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            MetaPill(label: rating, tone: tone),
            MetaPill(label: (model.measured ? t.lmSizesMeasured : t.lmSizes)(formatMegabytes(context, model.downloadMb), formatMegabytes(context, model.memoryMb))),
            if (model.vision) MetaPill(label: t.lmReadsImages, icon: Icons.image_outlined),
            InkWell(
              borderRadius: BorderRadius.circular(AppRadius.tag),
              onTap: () => setState(() => _open = !_open),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 3),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    Text(_open ? t.lmLess : t.lmDetails, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                    Icon(_open ? Icons.expand_less : Icons.expand_more, size: 16, color: palette.textMuted),
                  ],
                ),
              ),
            ),
          ],
        ),
        if (_open) ...<Widget>[
          const SizedBox(height: 10),
          Text('${t.lmLanguages}: $languages', style: TextStyle(color: palette.textSoft, fontSize: 12.5)),
          const SizedBox(height: 2),
          Text('${t.lmLicense}: ${model.license}', style: TextStyle(color: palette.textSoft, fontSize: 12.5)),
        ],
      ],
    );
    return Opacity(
      opacity: model.runs ? 1 : 0.6,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
        child: LayoutBuilder(
          builder: (context, constraints) {
            // On a phone the button goes under the text, so the details keep the full width.
            final narrow = constraints.maxWidth < 520;
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                _KindAvatar(kind: model.kind),
                const SizedBox(width: 14),
                Expanded(
                  child: narrow
                      ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: <Widget>[details, const SizedBox(height: 12), action])
                      : details,
                ),
                if (!narrow) ...<Widget>[const SizedBox(width: 16), action],
              ],
            );
          },
        ),
      ),
    );
  }
}
