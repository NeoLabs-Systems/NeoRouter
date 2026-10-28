// ignore: unused_import
import 'package:intl/intl.dart' as intl;
import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class AppL10nEn extends AppL10n {
  AppL10nEn([String locale = 'en']) : super(locale);

  @override
  String get appName => 'NeoRouter';

  @override
  String get brandTagline => 'INFERENCE ROUTER';

  @override
  String get cancel => 'Cancel';

  @override
  String get save => 'Save';

  @override
  String get delete => 'Delete';

  @override
  String get close => 'Close';

  @override
  String get create => 'Create';

  @override
  String get edit => 'Edit';

  @override
  String get retry => 'Try again';

  @override
  String get refresh => 'Refresh';

  @override
  String get copy => 'Copy';

  @override
  String get copied => 'Copied to clipboard.';

  @override
  String get saved => 'Saved.';

  @override
  String get enabled => 'Enabled';

  @override
  String get disabled => 'Disabled';

  @override
  String get never => 'never';

  @override
  String get unlimited => 'Unlimited';

  @override
  String get search => 'Search';

  @override
  String get free => 'Free';

  @override
  String get paid => 'Paid';

  @override
  String get unknown => 'Unknown';

  @override
  String get logout => 'Sign out';

  @override
  String get serverUnreachable => 'The server could not be reached.';

  @override
  String get groupOverview => 'Overview';

  @override
  String get groupSetup => 'Setup';

  @override
  String get groupRouting => 'Routing';

  @override
  String get navDashboard => 'Dashboard';

  @override
  String get navProviders => 'Providers';

  @override
  String get navModels => 'Models';

  @override
  String get navKeys => 'Router keys';

  @override
  String get navRouting => 'Auto router';

  @override
  String get navQuotas => 'Free quotas';

  @override
  String get navLogs => 'Request log';

  @override
  String get navPlayground => 'Playground';

  @override
  String get navSettings => 'Settings';

  @override
  String get authWelcome => 'Welcome back';

  @override
  String get authCreate => 'Create your account';

  @override
  String get authBootstrapTitle => 'Set up NeoRouter';

  @override
  String get authLoginSubtitle => 'Sign in to continue.';

  @override
  String get authRegisterSubtitle =>
      'Self-hosted. Your provider keys stay on your server.';

  @override
  String get authBootstrapSubtitle =>
      'The first account becomes the administrator.';

  @override
  String get username => 'Username';

  @override
  String get usernameOrEmail => 'Username or email';

  @override
  String get email => 'Email';

  @override
  String get password => 'Password';

  @override
  String get confirmPassword => 'Confirm password';

  @override
  String get signIn => 'Sign in';

  @override
  String get createAccount => 'Create account';

  @override
  String get toRegister => 'Don\'t have an account? Register';

  @override
  String get toLogin => 'Already have an account? Sign in';

  @override
  String get forgotPassword => 'Forgot password?';

  @override
  String get forgotPasswordTitle => 'Reset password';

  @override
  String get forgotPasswordBody =>
      'An administrator can reset it on the server with: neorouter reset-password <user> <new-password>';

  @override
  String get passwordsDontMatch => 'Passwords do not match.';

  @override
  String get passwordTooShort => 'Use a password with at least 12 characters.';

  @override
  String get registrationClosed =>
      'Registration is closed. Ask an administrator to open it.';

  @override
  String get dashboardEyebrow => 'Overview';

  @override
  String get dashboardTitle => 'Dashboard';

  @override
  String get dashboardDescription =>
      'Traffic, cost and health across every provider you connected.';

  @override
  String get range24h => '24 h';

  @override
  String get range7d => '7 days';

  @override
  String get range30d => '30 days';

  @override
  String get statRequests => 'Requests';

  @override
  String get statSuccessRate => 'Success rate';

  @override
  String get statCost => 'Spent';

  @override
  String get statTokens => 'Tokens';

  @override
  String get statFreeShare => 'Served free';

  @override
  String get statLatency => 'Avg. latency';

  @override
  String get statFallbacks => 'With fallback';

  @override
  String statInOut(String input, String output) {
    return '$input in · $output out';
  }

  @override
  String get topModels => 'Top models';

  @override
  String requestsShort(String count) {
    return '$count req.';
  }

  @override
  String get trafficTitle => 'Traffic';

  @override
  String get errorsTitle => 'Errors';

  @override
  String get noTrafficTitle => 'No traffic yet';

  @override
  String get noTrafficMessage =>
      'Send a request to your endpoint and it shows up here.';

  @override
  String get endpointTitle => 'Your endpoint';

  @override
  String get endpointHint =>
      'OpenAI-, Anthropic- and Responses-compatible. Authenticate with a router key.';

  @override
  String get quickStartTitle => 'Get started';

  @override
  String get quickStartProviders =>
      'Add a provider – the key is checked and all models are imported.';

  @override
  String get quickStartKeys =>
      'Create a router key with a budget or free-only mode.';

  @override
  String get quickStartUse =>
      'Use model \"auto\" or \"provider/model\" from any OpenAI or Anthropic SDK.';

  @override
  String get homeAllGood => 'Everything is routing smoothly';

  @override
  String get homeNeedsAttention => 'A provider needs your attention';

  @override
  String homeHealthyOf(String healthy, String total) {
    return '$healthy of $total providers healthy';
  }

  @override
  String get homeOpenPlayground => 'Open Playground';

  @override
  String get homeProblemHint =>
      'Requests skip it until this is fixed. Nothing else is affected.';

  @override
  String get homeFix => 'Fix';

  @override
  String get homeConnectTitle => 'Connect your app';

  @override
  String get homeConnectHint =>
      'Works with any OpenAI or Anthropic SDK. Ask for \"auto\" and NeoRouter picks the model.';

  @override
  String homeKeyHint(String name, String prefix) {
    return 'Use a router key, e.g. $name ($prefix…).';
  }

  @override
  String get homeManage => 'Manage';

  @override
  String get homeRecent => 'Recent requests';

  @override
  String get homeAllActivity => 'All activity';

  @override
  String get homeNoRequests =>
      'No requests yet. Send one from the Playground or your app.';

  @override
  String homeQuotaLeft(String percent) {
    return '$percent % quota left';
  }

  @override
  String homeResetsIn(String time) {
    return 'resets in $time';
  }

  @override
  String get homeOk => 'OK';

  @override
  String get homeRetried => 'Retried';

  @override
  String get providersEyebrow => 'Setup';

  @override
  String get providersTitle => 'Providers';

  @override
  String get providersDescription =>
      'Connect any inference API. Keys are verified, encrypted and their models imported automatically.';

  @override
  String get addProvider => 'Add provider';

  @override
  String get chooseProvider => 'Choose a provider';

  @override
  String get searchProviders => 'Search providers…';

  @override
  String get categoryFree => 'Free tiers';

  @override
  String get categoryAggregator => 'Aggregators';

  @override
  String get categoryPaid => 'Paid APIs';

  @override
  String get categoryLocal => 'Local';

  @override
  String get categoryCustom => 'Custom';

  @override
  String get categoryCommunity => 'Community';

  @override
  String get apiKey => 'API key';

  @override
  String get apiKeyOptional => 'API key (optional)';

  @override
  String get apiKeyKeep => 'API key (leave empty to keep the current one)';

  @override
  String get baseUrl => 'Base URL';

  @override
  String get displayNameLabel => 'Display name';

  @override
  String get slugLabel => 'Name in model ids';

  @override
  String slugHint(String slug) {
    return 'Models are addressed as $slug/<model>.';
  }

  @override
  String get billingMode => 'Billing';

  @override
  String get billingFree => 'Free tier – no payment method';

  @override
  String get billingPaid => 'Paid account';

  @override
  String get billingFreeWarning =>
      'Only choose the free tier if this account cannot be charged. NeoRouter then treats every model of this account as 100% free.';

  @override
  String get billingAlwaysFreeNote =>
      'Some models of this provider are always free, independent of billing.';

  @override
  String get getKey => 'Get a key';

  @override
  String get checkAndAdd => 'Check key & import models';

  @override
  String providerAdded(String count) {
    return 'Key verified – $count models imported.';
  }

  @override
  String providerAddedUnverified(String message) {
    return 'Saved. The key could not be verified yet: $message';
  }

  @override
  String get statusHealthy => 'Healthy';

  @override
  String get statusInvalid => 'Key rejected';

  @override
  String get statusError => 'Check failed';

  @override
  String get statusUnknown => 'Not checked';

  @override
  String get statusDisabled => 'Disabled';

  @override
  String modelsCount(String total, String free) {
    return '$total models · $free free';
  }

  @override
  String get revalidate => 'Check key';

  @override
  String get syncModels => 'Refresh models';

  @override
  String get probeQuota => 'Check balance';

  @override
  String get removeProvider => 'Remove provider';

  @override
  String removeProviderConfirm(String name) {
    return 'Remove $name? Its models disappear from routing; request history is kept.';
  }

  @override
  String get editProvider => 'Edit provider';

  @override
  String lastChecked(String time) {
    return 'Checked $time';
  }

  @override
  String syncedModels(String count) {
    return '$count models synced.';
  }

  @override
  String get noProvidersTitle => 'No providers yet';

  @override
  String get noProvidersMessage =>
      'Add your first provider to fill the model catalog.';

  @override
  String quotaProbeResult(String metric, String remaining) {
    return '$metric: $remaining';
  }

  @override
  String get quotaProbeEmpty => 'This provider does not report a balance.';

  @override
  String get modelsEyebrow => 'Catalog';

  @override
  String get modelsTitle => 'Models';

  @override
  String get modelsDescription =>
      'Every chat model your providers expose, with learned speed and reliability.';

  @override
  String get searchModels => 'Search models…';

  @override
  String get filterAll => 'All';

  @override
  String get filterFree => 'Free';

  @override
  String get filterEnabled => 'Enabled';

  @override
  String get filterTools => 'Tools';

  @override
  String get filterVision => 'Vision';

  @override
  String get filterIssues => 'Issues';

  @override
  String get intelligence => 'Intelligence';

  @override
  String get speed => 'Speed';

  @override
  String get reliability => 'Reliability';

  @override
  String get cost => 'Cost';

  @override
  String get price => 'Price';

  @override
  String pricePerMillion(String input, String output) {
    return '$input / $output per 1M';
  }

  @override
  String get priceUnknown => 'Price unknown';

  @override
  String contextWindow(String tokens) {
    return '$tokens context';
  }

  @override
  String get tools => 'Tools';

  @override
  String get vision => 'Vision';

  @override
  String get reasoning => 'Reasoning';

  @override
  String get freeReasonManualFree => 'Marked free by you';

  @override
  String get freeReasonManualPaid => 'Marked paid by you';

  @override
  String get freeReasonProviderFreeModel => 'Free model of this provider';

  @override
  String get freeReasonListedZeroPrice => 'Provider lists a price of 0';

  @override
  String get freeReasonFreeTierAccount => 'Free-tier account';

  @override
  String get freeReasonPaid => 'Paid';

  @override
  String get freeReasonPriceUnknown => 'Price unknown – not counted as free';

  @override
  String get modelSettings => 'Model settings';

  @override
  String get overrideFree => 'Free status';

  @override
  String get overrideAuto => 'Automatic';

  @override
  String get overrideForceFree => 'Always free';

  @override
  String get overrideForcePaid => 'Always paid';

  @override
  String get inputPrice => 'Input USD / 1M';

  @override
  String get outputPrice => 'Output USD / 1M';

  @override
  String get intelligenceOverride =>
      'Intelligence override (0–1, empty = automatic)';

  @override
  String intelligenceSource(String source) {
    return 'Source: $source';
  }

  @override
  String get resetHealth => 'Reset health';

  @override
  String get healthReset => 'Penalties and cooldowns cleared.';

  @override
  String get enableAll => 'Enable all';

  @override
  String get disableAll => 'Disable all';

  @override
  String get enableFreeOnly => 'Enable only free';

  @override
  String measured(String latency, String tps, String samples) {
    return '$latency · $tps tok/s · $samples calls';
  }

  @override
  String get notMeasured => 'Not measured yet';

  @override
  String penaltyLabel(String value) {
    return 'Penalty $value';
  }

  @override
  String unavailableUntil(String reason, String time) {
    return '$reason until $time';
  }

  @override
  String get noModelsTitle => 'No models';

  @override
  String get noModelsMessage => 'Add a provider or change the filter.';

  @override
  String modelsShown(String count) {
    return '$count models';
  }

  @override
  String get keysEyebrow => 'Access';

  @override
  String get keysTitle => 'Router keys';

  @override
  String get keysDescription =>
      'Keys for your apps. Each key has its own mode, budget and limits.';

  @override
  String get createKey => 'Create key';

  @override
  String get editKey => 'Edit key';

  @override
  String get keyName => 'Name';

  @override
  String get keyMode => 'Model access';

  @override
  String get modeFreeOnly => 'Only 100% free models';

  @override
  String get modePaid => 'All models, stop at the budget';

  @override
  String get modeBoth => 'All models, then free ones once the budget is used';

  @override
  String get budget => 'Budget (USD, empty = no limit)';

  @override
  String get budgetPeriod => 'Budget period';

  @override
  String get periodTotal => 'Total';

  @override
  String get periodMonthly => 'Per month';

  @override
  String get periodDaily => 'Per day';

  @override
  String get rpmLimit => 'Requests per minute (empty = no limit)';

  @override
  String get allowedModels =>
      'Allowed models – glob patterns, one per line (empty = all)';

  @override
  String get defaultProfile => 'Routing profile for \"auto\"';

  @override
  String get keySecretTitle => 'Your new router key';

  @override
  String get keySecretBody =>
      'Copy it now – it is stored only as a hash and cannot be shown again.';

  @override
  String get revoke => 'Revoke';

  @override
  String revokeConfirm(String name) {
    return 'Revoke $name? Apps using it stop working immediately.';
  }

  @override
  String get revoked => 'Revoked';

  @override
  String spentOf(String spent, String budget) {
    return '$spent of $budget';
  }

  @override
  String spentNoBudget(String spent) {
    return '$spent spent';
  }

  @override
  String resetsAt(String time) {
    return 'resets $time';
  }

  @override
  String lastUsed(String time) {
    return 'Last used $time';
  }

  @override
  String get noKeysTitle => 'No router keys';

  @override
  String get noKeysMessage => 'Create a key to call your endpoint.';

  @override
  String deleteKeyConfirm(String name) {
    return 'Delete $name permanently?';
  }

  @override
  String get routingEyebrow => 'Auto router';

  @override
  String get routingTitle => 'Routing';

  @override
  String get routingDescription =>
      'How \"auto\" picks a model. Scores combine intelligence, measured speed, learned reliability and price; failing or slow models are penalized and load is spread.';

  @override
  String get presetBalanced => 'Balanced';

  @override
  String get presetReliable => 'Most reliable';

  @override
  String get presetFast => 'Fastest';

  @override
  String get presetSmart => 'Smartest';

  @override
  String get presetBest => 'Best';

  @override
  String get presetCheap => 'Cheapest';

  @override
  String get presetCustom => 'Custom';

  @override
  String get presetSlider => 'Profile';

  @override
  String get weights => 'Weights';

  @override
  String get constraints => 'Constraints';

  @override
  String get customProfiles => 'Custom profiles';

  @override
  String get profileName => 'Profile name';

  @override
  String get saveProfile => 'Save as profile';

  @override
  String get updateProfile => 'Update profile';

  @override
  String profileUsage(String id) {
    return 'Use it as model \"auto:$id\" or set it as a key default.';
  }

  @override
  String get minIntelligence => 'Minimum intelligence';

  @override
  String get maxPriceOut => 'Max output price (USD / 1M, empty = any)';

  @override
  String get adaptive => 'Adapt to request complexity';

  @override
  String get adaptiveHint =>
      'Long, code-heavy or tool requests shift weight toward intelligence.';

  @override
  String get exploration => 'Load spreading';

  @override
  String get explorationHint =>
      'Higher values spread requests over more good models.';

  @override
  String get excludeProviders => 'Exclude providers (comma separated)';

  @override
  String get previewTitle => 'Live preview';

  @override
  String get previewHint =>
      'What \"auto\" would choose right now, without sending a request.';

  @override
  String get previewPrompt => 'Test prompt';

  @override
  String get previewFreeOnly => 'Free models only';

  @override
  String get previewTools => 'Needs tools';

  @override
  String get previewVision => 'Needs vision';

  @override
  String get runPreview => 'Preview';

  @override
  String get score => 'Score';

  @override
  String excluded(String reason) {
    return 'Excluded: $reason';
  }

  @override
  String get reasonNotFree => 'not free';

  @override
  String get reasonNoTools => 'no tool support';

  @override
  String get reasonNoVision => 'no vision';

  @override
  String get reasonContext => 'context too small';

  @override
  String get reasonCooldown => 'cooling down';

  @override
  String get reasonCircuit => 'paused after errors';

  @override
  String get reasonRateLimited => 'rate limited';

  @override
  String get reasonQuota => 'quota used up';

  @override
  String get reasonBudget => 'over budget';

  @override
  String get reasonPriceUnknown => 'price unknown';

  @override
  String get reasonDisabled => 'disabled';

  @override
  String get reasonAccount => 'provider unavailable';

  @override
  String get reasonAuth => 'key rejected';

  @override
  String get reasonNotAvailable => 'not available';

  @override
  String get reasonPayment => 'payment required';

  @override
  String get reasonNoFreeQuota => 'no free quota on this account';

  @override
  String get quotasEyebrow => 'Limits';

  @override
  String get quotasTitle => 'Free quotas';

  @override
  String get quotasDescription =>
      'Usage against each provider’s limits. Limits come from provider headers, documented free tiers or are learned from errors.';

  @override
  String get sharedLimits => 'Shared account limits';

  @override
  String get perModelLimits => 'Per model';

  @override
  String get metricRpm => 'requests / min';

  @override
  String get metricRpd => 'requests / day';

  @override
  String get metricTpm => 'tokens / min';

  @override
  String get metricTpd => 'tokens / day';

  @override
  String usedOf(String used, String limit) {
    return '$used of $limit';
  }

  @override
  String get sourceHeader => 'from provider';

  @override
  String get sourceCurated => 'documented';

  @override
  String get sourceLearned => 'learned';

  @override
  String get sourceManual => 'manual';

  @override
  String get sourceError => 'observed';

  @override
  String get balance => 'Balance';

  @override
  String get noQuotaData =>
      'No limits known yet. They appear after the first requests.';

  @override
  String onlyFreeModels(String count) {
    return 'Free models without known limits: $count';
  }

  @override
  String get logsEyebrow => 'Insights';

  @override
  String get logsTitle => 'Request log';

  @override
  String get logsDescription =>
      'Metadata only – prompts and answers are never stored.';

  @override
  String get filterErrors => 'Errors';

  @override
  String get filterSuccess => 'Successful';

  @override
  String get fallbackTrail => 'Fallback trail';

  @override
  String attempts(String count) {
    return '$count attempts';
  }

  @override
  String tokensInOut(String input, String output) {
    return '$input → $output tokens';
  }

  @override
  String ttft(String value) {
    return 'TTFT $value';
  }

  @override
  String get noLogsTitle => 'No requests yet';

  @override
  String get noLogsMessage =>
      'Requests appear here as soon as your apps call the endpoint.';

  @override
  String get playgroundEyebrow => 'Test';

  @override
  String get playgroundTitle => 'Playground';

  @override
  String get playgroundDescription =>
      'Chat through the real router with one of your keys.';

  @override
  String get selectKey => 'Router key';

  @override
  String get modelField => 'Model (auto, auto:fast, provider/model)';

  @override
  String get systemPrompt => 'System prompt (optional)';

  @override
  String get messageHint => 'Write a message…';

  @override
  String get send => 'Send';

  @override
  String get clearChat => 'Clear';

  @override
  String routedTo(String model, String latency) {
    return 'via $model · $latency';
  }

  @override
  String get playgroundNeedsKey => 'Create a router key first.';

  @override
  String get settingsTitle => 'Settings';

  @override
  String get settingsScopeYou => 'You';

  @override
  String get settingsScopeServer => 'This server';

  @override
  String get settingsAccountDescription => 'Your name, email and password.';

  @override
  String get settingsAppearanceDescription =>
      'Theme and language of this dashboard.';

  @override
  String get settingsConnectDescription =>
      'Ready-to-paste setup for your SDKs and tools.';

  @override
  String get settingsServerDescription =>
      'Sign-ups, people and model metadata. Only admins see this page.';

  @override
  String get settingsAppliesInstantly => 'Changes apply instantly';

  @override
  String get serverTitle => 'Server';

  @override
  String get profileTitle => 'Profile';

  @override
  String get themeLabel => 'Theme';

  @override
  String get themeHint => 'Follows your device unless you pick one.';

  @override
  String get languageHint =>
      'Only the dashboard. The gateway is language-neutral.';

  @override
  String get passwordHint =>
      'At least 12 characters. You will be signed out afterwards.';

  @override
  String get signOutHint => 'Sign out of NeoRouter in this browser.';

  @override
  String get appearance => 'Appearance';

  @override
  String get themeSystem => 'System';

  @override
  String get themeLight => 'Light';

  @override
  String get themeDark => 'Dark';

  @override
  String get language => 'Language';

  @override
  String get languageSystem => 'System';

  @override
  String get account => 'Account';

  @override
  String get displayName => 'Display name';

  @override
  String get saveProfileButton => 'Save profile';

  @override
  String get changePassword => 'Change password';

  @override
  String get securityTitle => 'Security';

  @override
  String get twoFactorTitle => 'Two-factor authentication';

  @override
  String get twoFactorOn => '2FA on';

  @override
  String get twoFactorOff => '2FA off';

  @override
  String get twoFactorDisabledHint =>
      'Ask for a code from an authenticator app at every sign-in.';

  @override
  String twoFactorEnabledHint(String count) {
    return 'Sign-in asks for an authenticator code. $count recovery codes left.';
  }

  @override
  String get twoFactorEnable => 'Turn on';

  @override
  String get twoFactorDisable => 'Turn off';

  @override
  String get twoFactorNewCodes => 'New recovery codes';

  @override
  String get twoFactorPasswordPrompt =>
      'Confirm your password to set up two-factor authentication.';

  @override
  String get twoFactorContinue => 'Continue';

  @override
  String get twoFactorScanHint =>
      'Scan the QR code with an authenticator app (1Password, Google Authenticator, Authy, …) or enter the key by hand. Then type the 6-digit code it shows.';

  @override
  String get twoFactorManualKey => 'Setup key';

  @override
  String get twoFactorCode => 'Authenticator code';

  @override
  String get twoFactorCodeHint => '6-digit code or a recovery code';

  @override
  String get twoFactorCodeOrRecovery => 'Authenticator or recovery code';

  @override
  String get twoFactorVerify => 'Verify';

  @override
  String get twoFactorCodesHint =>
      'Store these recovery codes somewhere safe. Each one works once if you lose your authenticator. They are shown only now.';

  @override
  String get twoFactorCodesSaved => 'I saved them';

  @override
  String get twoFactorDisablePrompt =>
      'Enter your password and a current code to turn off two-factor authentication.';

  @override
  String get twoFactorNewCodesPrompt =>
      'New codes replace all old ones. Enter your password and a current code.';

  @override
  String get twoFactorDisabledNotice => 'Two-factor authentication is off.';

  @override
  String get twoFactorBack => 'Use a different account';

  @override
  String get twoFactorLoginTitle => 'Two-step sign-in';

  @override
  String get twoFactorLoginSubtitle =>
      'Enter the code from your authenticator app.';

  @override
  String get currentPassword => 'Current password';

  @override
  String get newPassword => 'New password';

  @override
  String get passwordChanged => 'Password changed. Sign in again.';

  @override
  String get connectApps => 'Connect your apps';

  @override
  String get connectAppsHint => 'Replace nrt_… with one of your router keys.';

  @override
  String get registrationOpenLabel => 'Allow new registrations';

  @override
  String get registrationOpenHint =>
      'When off, only existing accounts can sign in.';

  @override
  String get users => 'Users';

  @override
  String get roleAdmin => 'Admin';

  @override
  String get roleUser => 'User';

  @override
  String get you => 'you';

  @override
  String get disableUser => 'Disable';

  @override
  String get enableUser => 'Enable';

  @override
  String get makeAdmin => 'Make admin';

  @override
  String get makeUser => 'Make user';

  @override
  String deleteUserConfirm(String name) {
    return 'Delete $name with all providers, keys and logs?';
  }

  @override
  String get metadataSources => 'Model metadata';

  @override
  String get metadataHint =>
      'Prices, context sizes and intelligence estimates come from models.dev and OpenRouter. An Artificial Analysis key adds benchmark-based intelligence scores.';

  @override
  String get aaKey => 'Artificial Analysis API key (optional)';

  @override
  String get aaConfigured => 'Artificial Analysis key is configured.';

  @override
  String get refreshMetadata => 'Refresh metadata now';

  @override
  String get metadataRefreshed => 'Metadata refreshed.';

  @override
  String lastFetched(String source, String time) {
    return '$source: $time';
  }

  @override
  String get notFetched => 'not fetched yet';

  @override
  String get navAnalytics => 'Analytics';

  @override
  String get analyticsEyebrow => 'Insights';

  @override
  String get analyticsTitle => 'Analytics';

  @override
  String get analyticsDescription =>
      'How your traffic, latency, cost and providers behave over time.';

  @override
  String get chartRequests => 'Requests';

  @override
  String get chartSuccess => 'Successful';

  @override
  String get chartErrors => 'Errors';

  @override
  String get chartLatency => 'Latency';

  @override
  String get chartP50 => 'p50';

  @override
  String get chartP95 => 'p95';

  @override
  String get chartTokens => 'Tokens';

  @override
  String get chartInput => 'Input';

  @override
  String get chartOutput => 'Output';

  @override
  String get chartCost => 'Cost (USD)';

  @override
  String get chartFreeShare => 'Served free';

  @override
  String get providerShare => 'Traffic by provider';

  @override
  String get modelTable => 'Models';

  @override
  String get keyUsage => 'Router keys';

  @override
  String get apiFormats => 'API formats';

  @override
  String get attemptOutcomes => 'Attempt outcomes';

  @override
  String get attemptOutcomesHint =>
      'Every upstream attempt, including fallbacks and hedges.';

  @override
  String get latencyDistribution => 'Latency distribution';

  @override
  String get colRequests => 'Requests';

  @override
  String get colSuccess => 'Success';

  @override
  String get colLatency => 'p50 / p95';

  @override
  String get colTokens => 'Tokens';

  @override
  String get colCost => 'Cost';

  @override
  String upTo(String value) {
    return '≤ $value';
  }

  @override
  String over(String value) {
    return '> $value';
  }

  @override
  String get analyticsTruncated =>
      'Showing the most recent 100,000 requests of this range.';

  @override
  String get fetchModels => 'Fetch models';

  @override
  String modelsFound(String count) {
    return '$count models found';
  }

  @override
  String modelsFetchFailed(String message) {
    return 'Could not list models: $message';
  }

  @override
  String get manualModels => 'Additional model ids (one per line)';

  @override
  String get manualModelsHint =>
      'Used when the endpoint has no /models list or misses some models.';

  @override
  String get authHeaderLabel => 'Key header (empty = Authorization: Bearer)';

  @override
  String get customHeaders => 'Extra headers (Name: value, one per line)';

  @override
  String get modelsPathLabel => 'Models path (empty = /models)';

  @override
  String get advancedOptions => 'Advanced';

  @override
  String keyStatus(String status) {
    return 'Key: $status';
  }

  @override
  String get connectOpenRouter => 'Connect with OpenRouter';

  @override
  String get connectOpenRouterHint =>
      'One click: sign in at OpenRouter and the key is created and stored automatically.';

  @override
  String get orManualKey => 'or paste a key';

  @override
  String get openRouterConnected => 'OpenRouter connected – models imported.';

  @override
  String openRouterFailed(String message) {
    return 'OpenRouter connection failed: $message';
  }

  @override
  String get navAdmin => 'Admin';

  @override
  String get adminDescription =>
      'Run this server: accounts, sign-ups, server details and model metadata.';

  @override
  String get adminSearchHint => 'Search admin settings, e.g. registration';

  @override
  String get nothingMatches => 'Nothing matches';

  @override
  String get registrationTitle => 'Sign-ups';

  @override
  String usersCount(String count) {
    return '$count accounts on this server.';
  }

  @override
  String get serverInfoHint =>
      'What this NeoRouter instance runs and where apps reach it.';

  @override
  String get versionLabel => 'Version';

  @override
  String get capabilitiesLabel => 'Capabilities';

  @override
  String get settingsSearchHint => 'Search settings';

  @override
  String get settingsScopeApp => 'This app';

  @override
  String get settingsScopeAppHint => 'Applies to this browser';

  @override
  String get settingsScopeYouHint => 'Saved to your account';

  @override
  String get settingsSavedOnSubmit => 'Saved when you press Save';
}
