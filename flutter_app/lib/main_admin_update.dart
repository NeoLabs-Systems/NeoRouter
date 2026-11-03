// Updating NeoRouter from the admin pages: the banner that offers a new
// version (and follows the update while it runs) and the updates settings.
// The server runs the same flow as `neorouter update`; this only shows it.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/page_reload.dart';
import 'src/router_api.dart';

/// What the server knows about updates: the installed version, a newer one
/// waiting on the channel, and the update job when one ran.
class UpdateStatus {
  UpdateStatus(Map<String, dynamic> json)
      : version = '${json['version'] ?? ''}',
        channel = '${json['channel'] ?? 'beta'}',
        selfUpdate = json['selfUpdate'] == true,
        blockedReason = json['blockedReason'] as String?,
        checkedAt = DateTime.tryParse('${json['checkedAt'] ?? ''}')?.toLocal(),
        checkFailed = json['checkFailed'] == true,
        availableVersion = (json['available'] as Map?)?['version'] as String?,
        changes = ((json['available'] as Map?)?['changes'] as List? ?? const <Object>[]).map((item) => '$item').toList(),
        state = '${(json['job'] as Map?)?['state'] ?? 'idle'}',
        progress = (((json['job'] as Map?)?['progress'] as num?) ?? 0).toInt(),
        message = '${(json['job'] as Map?)?['message'] ?? ''}',
        versionBefore = (json['job'] as Map?)?['versionBefore'] as String?,
        versionAfter = (json['job'] as Map?)?['versionAfter'] as String?,
        rolledBack = (json['job'] as Map?)?['rolledBack'] == true,
        logs = ((json['job'] as Map?)?['logs'] as List? ?? const <Object>[]).map((line) => '$line').toList(),
        completedAt = DateTime.tryParse('${(json['job'] as Map?)?['completedAt'] ?? ''}')?.toLocal(),
        hasAvailable = json['available'] != null;

  final String version;
  final String channel;
  final bool selfUpdate;
  final String? blockedReason;
  final DateTime? checkedAt;
  final bool checkFailed;
  final String? availableVersion;
  final List<String> changes;
  final bool hasAvailable;
  final String state;
  final int progress;
  final String message;
  final String? versionBefore;
  final String? versionAfter;
  final bool rolledBack;
  final List<String> logs;
  final DateTime? completedAt;

  bool get running => state == 'running';
}

/// Loads the update status, starts updates and follows them to the end, also
/// across the server restart in the middle of one.
class UpdateWatcher extends ChangeNotifier {
  UpdateWatcher(this._api);

  final RouterApi _api;
  UpdateStatus? status;
  bool busy = false;
  bool reconnecting = false;
  String? error;
  DateTime? _dismissedAt;
  bool _followed = false;
  bool _disposed = false;
  Timer? _timer;

  /// A failed update is news for half an hour, or until it is dismissed. A
  /// finished one is only news on the page that watched it run: after the
  /// reload it has been delivered.
  bool get finishedRecently {
    final done = status?.completedAt;
    if (done == null || done == _dismissedAt) return false;
    final state = status!.state;
    if (state == 'completed') return _followed;
    return state == 'failed' && DateTime.now().difference(done) < const Duration(minutes: 30);
  }

  bool get justUpdated => _followed && status?.state == 'completed';

  void dismiss() {
    _dismissedAt = status?.completedAt;
    notifyListeners();
  }

  Future<void> load() => _fetch(() => _api.updateStatus());

  Future<void> check() => _guard(() => _fetch(() => _api.checkForUpdate(), rethrowErrors: true));

  Future<void> setChannel(String channel) => _guard(() => _fetch(() => _api.setUpdateChannel(channel), rethrowErrors: true));

  Future<void> start() => _guard(() async {
        _followed = true;
        await _fetch(() => _api.startUpdate(), rethrowErrors: true);
      });

  Future<void> _guard(Future<void> Function() action) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      await action();
    } on ApiException catch (failure) {
      error = failure.message;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  Future<void> _fetch(Future<Map<String, dynamic>> Function() request, {bool rethrowErrors = false}) async {
    try {
      status = UpdateStatus(await request());
      reconnecting = false;
      if (status!.running) _followed = true;
    } on ApiException catch (failure) {
      // The server going away mid-update is expected; anything else is shown.
      final gone = failure.status == null || failure.status! >= 502;
      if (_followed && gone) {
        reconnecting = true;
      } else if (rethrowErrors) {
        rethrow;
      }
    }
    _schedule();
    if (!_disposed) notifyListeners();
  }

  void _schedule() {
    _timer?.cancel();
    if (_disposed || !(status?.running == true || reconnecting)) return;
    _timer = Timer(const Duration(seconds: 2), load);
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    super.dispose();
  }
}

String channelLabel(AppL10n t, String channel) => channel == 'stable' ? t.channelStable : t.channelBeta;

/// The banner above every admin tab. It offers the update, then shows its
/// progress, then the result; with nothing to say it takes no space.
class UpdateBanner extends StatefulWidget {
  const UpdateBanner({super.key, required this.watcher});

  final UpdateWatcher watcher;

  @override
  State<UpdateBanner> createState() => _UpdateBannerState();
}

class _UpdateBannerState extends State<UpdateBanner> {
  bool _showChanges = false;
  bool _reloading = false;

  UpdateWatcher get watcher => widget.watcher;

  Future<void> _confirmAndStart(AppL10n t, UpdateStatus status) async {
    final confirmed = await confirmAction(
      context,
      title: t.updateConfirmTitle(status.availableVersion ?? ''),
      message: t.updateConfirmMessage,
      confirmLabel: t.updateNow,
      cancelLabel: t.cancel,
      destructive: false,
    );
    if (confirmed) await watcher.start();
  }

  void _reloadSoon() {
    if (_reloading) return;
    _reloading = true;
    Timer(const Duration(seconds: 3), reloadPage);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ListenableBuilder(
      listenable: watcher,
      builder: (context, _) {
        final status = watcher.status;
        if (status == null) return const SizedBox.shrink();
        if (status.running || watcher.reconnecting) return _frame(palette, palette.accent, _running(t, palette, status));
        if (watcher.finishedRecently) {
          if (status.state == 'completed') {
            if (watcher.justUpdated) _reloadSoon();
            return _frame(palette, palette.success, _completed(t, palette, status));
          }
          return _frame(palette, palette.error, _failed(t, palette, status));
        }
        if (status.hasAvailable && status.selfUpdate) return _frame(palette, palette.accent, _available(t, palette, status));
        return const SizedBox.shrink();
      },
    );
  }

  Widget _frame(NeoRouterPalette palette, Color tone, Widget child) => Padding(
        padding: const EdgeInsets.only(bottom: 18),
        child: Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(
            color: palette.surfaceRaised,
            borderRadius: BorderRadius.circular(AppRadius.card),
            border: Border.all(color: tone.withValues(alpha: 0.45)),
          ),
          child: child,
        ),
      );

  Widget _heading(NeoRouterPalette palette, IconData icon, Color tone, String title, String? subtitle, {Widget? trailing}) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Icon(icon, size: 20, color: tone),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(title, style: GoogleFonts.geist(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text)),
              if (subtitle != null && subtitle.isNotEmpty) ...<Widget>[
                const SizedBox(height: 3),
                Text(subtitle, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
              ],
            ],
          ),
        ),
        if (trailing != null) ...<Widget>[const SizedBox(width: 12), trailing],
      ],
    );
  }

  Widget _available(AppL10n t, NeoRouterPalette palette, UpdateStatus status) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _heading(
          palette,
          Icons.system_update_alt,
          palette.accent,
          t.updateReadyTitle(status.availableVersion ?? ''),
          t.updateReadySubtitle(status.version, channelLabel(t, status.channel)),
          trailing: Wrap(
            spacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              if (status.changes.isNotEmpty)
                TextButton(onPressed: () => setState(() => _showChanges = !_showChanges), child: Text(t.updateWhatsNew)),
              FilledButton(onPressed: watcher.busy ? null : () => _confirmAndStart(t, status), child: Text(t.updateNow)),
            ],
          ),
        ),
        if (_showChanges)
          Padding(
            padding: const EdgeInsets.only(left: 32, top: 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                for (final change in status.changes)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 4),
                    child: Text('•  $change', style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.4)),
                  ),
              ],
            ),
          ),
        if (watcher.error != null) _error(palette, watcher.error!),
      ],
    );
  }

  Widget _running(AppL10n t, NeoRouterPalette palette, UpdateStatus status) {
    final target = status.versionAfter;
    final detail = watcher.reconnecting ? t.updateReconnecting : status.message;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _heading(
          palette,
          Icons.sync,
          palette.accent,
          target == null || target.isEmpty ? t.updatingTitle : t.updatingToTitle(target),
          detail,
        ),
        const SizedBox(height: 12),
        ClipRRect(
          borderRadius: BorderRadius.circular(AppRadius.pill),
          child: LinearProgressIndicator(
            value: watcher.reconnecting ? null : status.progress / 100,
            minHeight: 5,
            backgroundColor: palette.border,
            valueColor: AlwaysStoppedAnimation<Color>(palette.accent),
          ),
        ),
        const SizedBox(height: 10),
        Text(t.updateHandsOff, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
      ],
    );
  }

  Widget _completed(AppL10n t, NeoRouterPalette palette, UpdateStatus status) {
    return _heading(
      palette,
      Icons.check_circle_outline,
      palette.success,
      t.updateDoneTitle(status.versionAfter ?? status.version),
      t.updateDoneReloading,
      trailing: Wrap(
        spacing: 8,
        children: <Widget>[
          TextButton(onPressed: watcher.dismiss, child: Text(t.close)),
          FilledButton(onPressed: reloadPage, child: Text(t.updateReload)),
        ],
      ),
    );
  }

  Widget _failed(AppL10n t, NeoRouterPalette palette, UpdateStatus status) {
    return _heading(
      palette,
      Icons.error_outline,
      palette.error,
      t.updateFailedTitle,
      status.message,
      trailing: Wrap(
        spacing: 8,
        children: <Widget>[
          TextButton(onPressed: watcher.dismiss, child: Text(t.close)),
          if (status.hasAvailable && status.selfUpdate)
            FilledButton(onPressed: watcher.busy ? null : () => _confirmAndStart(t, status), child: Text(t.retry)),
        ],
      ),
    );
  }

  Widget _error(NeoRouterPalette palette, String message) => Padding(
        padding: const EdgeInsets.only(left: 32, top: 10),
        child: Text(message, style: TextStyle(fontSize: 13, color: palette.error)),
      );
}

/// The updates block of the Server tab: channel, check now, why an install
/// cannot update itself, and the log of the last run.
class UpdateSettings extends StatelessWidget {
  const UpdateSettings({super.key, required this.watcher});

  final UpdateWatcher watcher;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return ListenableBuilder(
      listenable: watcher,
      builder: (context, _) {
        final status = watcher.status;
        if (status == null) return const SizedBox.shrink();
        final locked = status.running || watcher.busy || !status.selfUpdate;
        final checked = status.checkedAt == null ? t.updateNeverChecked : t.lastChecked(formatAgo(t, status.checkedAt!));
        return SettingsGroup(
          title: t.updatesTitle,
          description: t.updatesHint,
          trailing: OutlinedButton.icon(
            onPressed: locked ? null : watcher.check,
            icon: watcher.busy
                ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.refresh, size: 18),
            label: Text(t.updateCheckNow),
          ),
          children: <Widget>[
            SettingsRow(
              label: t.versionLabel,
              description: status.hasAvailable
                  ? t.updateReadyTitle(status.availableVersion ?? '')
                  : status.checkFailed
                      ? t.updateCheckFailed
                      : status.checkedAt == null
                          ? checked
                          : '${t.updateUpToDate} · $checked',
              control: Text(status.version, style: GoogleFonts.geistMono(color: palette.textSoft, fontSize: 13)),
            ),
            SettingsRow(
              label: t.updateChannel,
              description: t.updateChannelHint,
              control: SegmentedButton<String>(
                showSelectedIcon: false,
                segments: <ButtonSegment<String>>[
                  ButtonSegment<String>(value: 'stable', label: Text(t.channelStable)),
                  ButtonSegment<String>(value: 'beta', label: Text(t.channelBeta)),
                ],
                selected: <String>{status.channel},
                onSelectionChanged: locked ? null : (selection) => watcher.setChannel(selection.first),
              ),
            ),
            if (!status.selfUpdate)
              SettingsRow(
                label: status.blockedReason == 'container' ? t.updateContainerTitle : t.updateNotGitTitle,
                description: status.blockedReason == 'container' ? t.updateContainerHint : t.updateNotGitHint,
                control: status.blockedReason == 'container'
                    ? SelectableText('docker compose pull\ndocker compose up -d', style: GoogleFonts.geistMono(color: palette.textSoft, fontSize: 13))
                    : null,
              ),
            if (status.logs.isNotEmpty)
              ExpansionTile(
                tilePadding: const EdgeInsets.symmetric(horizontal: 20),
                shape: const Border(),
                collapsedShape: const Border(),
                title: Text(t.updateDetails, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: palette.text)),
                children: <Widget>[
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
                    child: Align(
                      alignment: Alignment.centerLeft,
                      child: SelectableText(status.logs.join('\n'), style: GoogleFonts.geistMono(color: palette.textSoft, fontSize: 12, height: 1.5)),
                    ),
                  ),
                ],
              ),
            if (watcher.error != null) SettingsRow(label: watcher.error!),
          ],
        );
      },
    );
  }
}
