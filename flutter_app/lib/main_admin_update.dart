// Updating NeoRouter from the admin pages. The banner, the settings and the
// follow-the-restart logic are shared; this supplies the server calls and the
// wording. The server runs the same flow as `neorouter update`.

import 'package:neolabs_flutter/neolabs_flutter.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_shared.dart';
import 'src/backend_client.dart';
import 'src/router_api.dart';

NeoUpdateController updateController(RouterApi api) => NeoUpdateController(
  NeoUpdateApi(
    status: api.updateStatus,
    check: api.checkForUpdate,
    start: api.startUpdate,
    setChannel: api.setUpdateChannel,
    failureOf: (error) => error is ApiException
        ? (status: error.status, message: error.message)
        : null,
  ),
);

NeoUpdateStrings updateStrings(AppL10n t) => NeoUpdateStrings(
  title: t.updatesTitle,
  description: t.updatesHint,
  versionLabel: t.versionLabel,
  channelLabel: t.updateChannel,
  channelHint: t.updateChannelHint,
  stable: t.channelStable,
  beta: t.channelBeta,
  checkNow: t.updateCheckNow,
  neverChecked: t.updateNeverChecked,
  checkedAt: (time) => t.lastChecked(formatAgo(t, time)),
  upToDate: t.updateUpToDate,
  checkFailed: t.updateCheckFailed,
  details: t.updateDetails,
  blocked: (reason) => reason == 'container'
      ? (title: t.updateContainerTitle, hint: t.updateContainerHint)
      : (title: t.updateNotGitTitle, hint: t.updateNotGitHint),
  readyTitle: t.updateReadyTitle,
  readySubtitle: t.updateReadySubtitle,
  whatsNew: t.updateWhatsNew,
  updateNow: t.updateNow,
  confirmTitle: t.updateConfirmTitle,
  confirmMessage: t.updateConfirmMessage,
  cancel: t.cancel,
  updatingTitle: t.updatingTitle,
  updatingToTitle: t.updatingToTitle,
  reconnecting: t.updateReconnecting,
  handsOff: t.updateHandsOff,
  doneTitle: t.updateDoneTitle,
  doneReloading: t.updateDoneReloading,
  reload: t.updateReload,
  failedTitle: t.updateFailedTitle,
  close: t.close,
  retry: t.retry,
);
