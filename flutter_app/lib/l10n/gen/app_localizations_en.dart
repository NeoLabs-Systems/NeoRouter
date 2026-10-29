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
  String get navProviders => 'Connections';

  @override
  String get navModels => 'Models';

  @override
  String get navKeys => 'API keys';

  @override
  String get navRouting => 'Smart routing';

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
  String get noTrafficTitle => 'Nothing here yet';

  @override
  String get noTrafficMessage =>
      'Once your apps send requests, you will see how much, how fast and which models answered.';

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
  String get homeProblemHint => 'Requests skip it until this is fixed.';

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
      'No requests yet. Send your first message in the Playground.';

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
  String get providersTitle => 'Connections';

  @override
  String get providersDescription =>
      'The AI providers NeoRouter can use. Keys are checked when you add them and stored encrypted.';

  @override
  String get addProvider => 'Add connection';

  @override
  String get chooseProvider => 'Add a connection';

  @override
  String get searchProviders => 'Search providers…';

  @override
  String get categoryFree => 'Free to start';

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
  String get categorySearch => 'Web search';

  @override
  String get categoryPlan => 'Subscriptions';

  @override
  String get kindFree => 'Free models';

  @override
  String get kindFreeHint => 'Never cost anything.';

  @override
  String get kindPlan => 'Prepaid plans';

  @override
  String get kindPlanHint =>
      'Use the allowance of plans you already pay for, like ChatGPT.';

  @override
  String get kindPaid => 'Paid models';

  @override
  String get kindPaidHint => 'Billed per use, within the spending limit below.';

  @override
  String get kindFreeShort => 'Free';

  @override
  String get kindPlanShort => 'Plans';

  @override
  String get kindPaidShort => 'Paid';

  @override
  String get pickOneKind => 'Pick at least one kind of model.';

  @override
  String get spendingLimitPer => 'per';

  @override
  String get budgetFallsBack =>
      'Empty means no limit. Once it is used up, the key keeps going with free models and plans.';

  @override
  String get budgetStops =>
      'Empty means no limit. Once it is used up, the key stops until the reset.';

  @override
  String get chatgptSignIn => 'Sign in with ChatGPT';

  @override
  String get chatgptSignInAgain => 'Sign in again';

  @override
  String get chatgptSignInHint =>
      'NeoRouter shows a code. Enter it on the OpenAI page with the account that has your ChatGPT plan.';

  @override
  String get chatgptEnterCode => 'Enter this code at OpenAI';

  @override
  String get chatgptOpenPage => 'Open the OpenAI page';

  @override
  String get chatgptWaiting => 'Waiting for you to confirm…';

  @override
  String get chatgptExpired => 'The code expired. Start again.';

  @override
  String get chatgptConnected => 'ChatGPT connected';

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
      'Choose the free plan only if no payment method is on file with this provider. NeoRouter double-checks where it can.';

  @override
  String get billingAlwaysFreeNote =>
      'Some models of this provider are always free, independent of billing.';

  @override
  String get getKey => 'Get a key';

  @override
  String get checkAndAdd => 'Connect';

  @override
  String providerAdded(String count) {
    return 'Key verified – $count models imported.';
  }

  @override
  String providerAddedUnverified(String message) {
    return 'Saved. The key could not be verified yet: $message';
  }

  @override
  String get statusHealthy => 'Working';

  @override
  String get statusInvalid => 'Key rejected';

  @override
  String get statusError => 'Not reachable';

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
  String get removeProvider => 'Remove connection';

  @override
  String removeProviderConfirm(String name) {
    return 'Remove $name? Its models are no longer used. You can add it again at any time.';
  }

  @override
  String get editProvider => 'Edit';

  @override
  String lastChecked(String time) {
    return 'Checked $time';
  }

  @override
  String syncedModels(String count) {
    return '$count models synced.';
  }

  @override
  String get noProvidersTitle => 'No connections yet';

  @override
  String get noProvidersMessage =>
      'Connect an AI provider and NeoRouter imports all of its models.';

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
      'Everything your connections offer. NeoRouter learns how smart, fast and reliable each model is and picks the best one for you.';

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
  String get filterIssues => 'Problems';

  @override
  String get intelligence => 'Smart';

  @override
  String get speed => 'Fast';

  @override
  String get reliability => 'Reliable';

  @override
  String get cost => 'Cheap';

  @override
  String get price => 'Price';

  @override
  String pricePerMillion(String input, String output) {
    return '$input / $output per 1M tokens';
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
  String get freeReasonBilledObserved =>
      'Paid: the provider reported a charge for it';

  @override
  String get freeReasonNoFreeTier => 'Paid: no free quota on this account';

  @override
  String get freeReasonNotInFreeTier => 'Paid: not part of the free tier';

  @override
  String get freeReasonBillingActive =>
      'Paid: billing is active on this account';

  @override
  String get freeReasonBillingUnverified =>
      'Not free until the free plan is verified';

  @override
  String get billingVerifiedFree => 'Free plan verified';

  @override
  String get billingDetectedPaid => 'Billing active';

  @override
  String get billingUnverified => 'Free plan not verified';

  @override
  String get modelSettings => 'Model settings';

  @override
  String get overrideFree => 'Free or paid';

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
  String get enableAll => 'Turn all on';

  @override
  String get disableAll => 'Turn all off';

  @override
  String get enableFreeOnly => 'Use only free models';

  @override
  String measured(String latency, String tps, String samples) {
    return '$latency · $tps tokens/s · $samples requests';
  }

  @override
  String get notMeasured => 'Not used yet';

  @override
  String penaltyLabel(String value) {
    return 'Penalty $value';
  }

  @override
  String unavailableUntil(String reason, String time) {
    return '$reason until $time';
  }

  @override
  String get noModelsTitle => 'No models found';

  @override
  String get noModelsMessage =>
      'Try another filter or connect another provider.';

  @override
  String modelsShown(String count) {
    return '$count models';
  }

  @override
  String get keysEyebrow => 'Access';

  @override
  String get keysTitle => 'API keys';

  @override
  String get keysDescription =>
      'Keys let your apps use NeoRouter. Decide per key whether it may use paid models and how much it may spend.';

  @override
  String get createKey => 'Create key';

  @override
  String get editKey => 'Edit key';

  @override
  String get keyName => 'Name';

  @override
  String get keyMode => 'Model access';

  @override
  String get budget => 'Limit in USD (empty = no limit)';

  @override
  String get budgetPeriod => 'Budget period';

  @override
  String get periodTotal => 'one-time';

  @override
  String get periodMonthly => 'month';

  @override
  String get periodDaily => 'day';

  @override
  String get budgetResets => 'Budget applies';

  @override
  String get budgetResetsHint => 'A one-time budget does not refill.';

  @override
  String get paceAhead => 'Plenty left: better models are allowed';

  @override
  String get paceOnTrack => 'On track for the period';

  @override
  String paceTight(String forecast) {
    return 'At this pace about $forecast by the reset: NeoRouter prefers cheaper models';
  }

  @override
  String get paceOver => 'Used up until the reset';

  @override
  String get rpmLimit => 'Requests per minute (empty = no limit)';

  @override
  String get allowedModels =>
      'Allowed models – glob patterns, one per line (empty = all)';

  @override
  String get defaultProfile => 'Routing style for \"auto\"';

  @override
  String get keySecretTitle => 'Copy your new key';

  @override
  String get keySecretBody =>
      'For your security it is shown only once. Paste it into your app where it asks for an API key.';

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
    return '$spent of $budget spent';
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
  String get noKeysTitle => 'No API keys yet';

  @override
  String get noKeysMessage =>
      'Create a key and paste it into any app that works with OpenAI or Claude.';

  @override
  String deleteKeyConfirm(String name) {
    return 'Delete $name permanently?';
  }

  @override
  String get routingEyebrow => 'Auto router';

  @override
  String get routingTitle => 'Smart routing';

  @override
  String get routingDescription =>
      'When an app asks for \"auto\", NeoRouter picks the model for each request. Choose what matters most to you.';

  @override
  String get presetBalanced => 'Balanced';

  @override
  String get presetFast => 'Fastest';

  @override
  String get presetSmart => 'Smartest';

  @override
  String get presetCustom => 'My style';

  @override
  String get presetSlider => 'Profile';

  @override
  String get weights => 'Weights';

  @override
  String get constraints => 'Constraints';

  @override
  String get customProfiles => 'Custom profiles';

  @override
  String get profileName => 'Name';

  @override
  String get saveProfile => 'Save as profile';

  @override
  String get updateProfile => 'Update profile';

  @override
  String profileUsage(String id) {
    return 'Use it as model \"auto:$id\" or set it as a key default.';
  }

  @override
  String get previewTitle => 'Right now it would choose';

  @override
  String previewHint(String style) {
    return 'Which models \"$style\" would choose right now – nothing is sent.';
  }

  @override
  String get previewPrompt => 'Test prompt';

  @override
  String get previewFreeOnly => 'Only free models';

  @override
  String get previewTools => 'Needs tools';

  @override
  String get previewVision => 'Needs vision';

  @override
  String get runPreview => 'Check';

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
  String get metricRpm => 'Requests per minute';

  @override
  String get metricRpd => 'Requests per day';

  @override
  String get metricTpm => 'Tokens per minute';

  @override
  String get metricTpd => 'Tokens per day';

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
      'No limits reported yet. They appear after the first requests.';

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
  String get selectKey => 'API key';

  @override
  String get modelField => 'Model (auto, auto:fast, provider/model)';

  @override
  String get systemPrompt => 'System prompt (optional)';

  @override
  String get messageHint => 'Message NeoRouter…';

  @override
  String get send => 'Send';

  @override
  String get clearChat => 'Clear';

  @override
  String routedTo(String model, String latency) {
    return '$model · $latency';
  }

  @override
  String get playgroundNeedsKey =>
      'Create an API key first – the Playground uses it just like your apps do.';

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

  @override
  String get navHome => 'Home';

  @override
  String get navActivity => 'Activity';

  @override
  String get navSectionServer => 'Server';

  @override
  String get capChat => 'Chat';

  @override
  String get capEmbedding => 'Embeddings';

  @override
  String get capRerank => 'Ranking';

  @override
  String get capImage => 'Images';

  @override
  String get capAudio => 'Audio';

  @override
  String get capSpeech => 'Speech';

  @override
  String get capSearch => 'Web search';

  @override
  String get capTranscription => 'Transcription';

  @override
  String get capVideo => 'Video';

  @override
  String get capModeration => 'System One models';

  @override
  String get capOther => 'Other';

  @override
  String get capChatHint => 'Text and vision models that answer messages.';

  @override
  String get capEmbeddingHint =>
      'Turn text into vectors for search and memory.';

  @override
  String get capRerankHint => 'Sort search results by relevance.';

  @override
  String get capImageHint => 'Create and edit images.';

  @override
  String get capAudioHint => 'Speech to text, text to speech and music.';

  @override
  String get capSpeechHint => 'Read text aloud in a natural voice.';

  @override
  String get capSearchHint =>
      'Find current pages on the web, for you and as a tool for chat models.';

  @override
  String get capTranscriptionHint => 'Turn recordings into text and subtitles.';

  @override
  String get capVideoHint => 'Create short videos from a description.';

  @override
  String get capLive => 'Live voice';

  @override
  String get capLiveHint =>
      'Talk in real time: the model listens and answers with its own voice.';

  @override
  String get capModerationHint =>
      'Fast decision models such as Jev: they classify, check and decide in a split second instead of writing long answers.';

  @override
  String get capOtherHint =>
      'Specialised models such as translation or document parsing.';

  @override
  String capComingSoon(String count) {
    return 'NeoRouter already found $count of these models in your connections and will route them in a coming update.';
  }

  @override
  String get capSoon => 'Soon';

  @override
  String greetMorning(String name) {
    return 'Good morning, $name';
  }

  @override
  String greetAfternoon(String name) {
    return 'Good afternoon, $name';
  }

  @override
  String greetEvening(String name) {
    return 'Good evening, $name';
  }

  @override
  String homeStatusOk(String count) {
    return 'Everything is running. $count connections are working.';
  }

  @override
  String homeStatusIssues(String count) {
    return '$count connection needs your attention. Requests use the others meanwhile.';
  }

  @override
  String get homeStatusEmpty => 'Connect an AI provider to get started.';

  @override
  String get homeLast7Days => 'Last 7 days';

  @override
  String get metricRequests => 'Requests';

  @override
  String get metricAnswered => 'Answered';

  @override
  String get metricSpent => 'Spent';

  @override
  String metricFreeShare(String percent) {
    return '$percent % free';
  }

  @override
  String get metricResponse => 'Avg. response time';

  @override
  String get homeSeeAll => 'See all';

  @override
  String get homeRecentActivity => 'Recent activity';

  @override
  String homeModelsLine(String total, String free) {
    return '$total models · $free free';
  }

  @override
  String get homeUseTitle => 'Use NeoRouter in your apps';

  @override
  String get homeUseBody =>
      'Paste this address and one of your API keys into any app that works with OpenAI or Claude.';

  @override
  String get homeGetKey => 'Get an API key';

  @override
  String get homeShowExamples => 'Show code examples';

  @override
  String get homeHideExamples => 'Hide code examples';

  @override
  String get setupTitle => 'Finish setting up';

  @override
  String setupProgress(String done, String total) {
    return '$done of $total done';
  }

  @override
  String get setupStep1Title => 'Connect an AI provider';

  @override
  String get setupStep1Body =>
      'Add a key from Groq, Google, OpenRouter or another provider. NeoRouter finds all its models.';

  @override
  String get setupStep2Title => 'Create an API key';

  @override
  String get setupStep2Body =>
      'One key for your apps, optionally limited to free models or a budget.';

  @override
  String get setupStep3Title => 'Send your first message';

  @override
  String get setupStep3Body => 'Try it in the Playground.';

  @override
  String get setupStart => 'Start';

  @override
  String problemTitle(String name) {
    return '$name needs attention';
  }

  @override
  String get problemKeyRejected =>
      'The provider rejected the key. Update it to use this connection again.';

  @override
  String requestFailed(String reason) {
    return 'Failed: $reason';
  }

  @override
  String get agoNow => 'just now';

  @override
  String agoMinutes(String count) {
    return '$count min ago';
  }

  @override
  String agoHours(String count) {
    return '$count h ago';
  }

  @override
  String get activityDescription =>
      'What your apps asked for and how NeoRouter answered. Only metadata is kept – never your messages.';

  @override
  String get tabOverview => 'Overview';

  @override
  String get tabRequests => 'Requests';

  @override
  String metricTokens(String count) {
    return '$count tokens';
  }

  @override
  String get metricTypicalResponse => 'Typical response';

  @override
  String metricSlowest(String value) {
    return 'slowest 5 %: $value';
  }

  @override
  String get chartResponseTime => 'Response time';

  @override
  String get chartTypical => 'Typical';

  @override
  String get chartSlowest => 'Slowest 5 %';

  @override
  String get topModelsTitle => 'Most used models';

  @override
  String get byConnection => 'By connection';

  @override
  String get byKey => 'By API key';

  @override
  String rankingLine(String count, String latency) {
    return '$count · $latency';
  }

  @override
  String tooltipRequests(String count, String errors) {
    return '$count requests · $errors failed';
  }

  @override
  String get filterAnswered => 'Answered';

  @override
  String get filterFailed => 'Failed';

  @override
  String get logsPrivacyHint =>
      'NeoRouter never stores what you or the model wrote – only which model answered, how fast and what it cost.';

  @override
  String get detailRequested => 'Asked for';

  @override
  String get detailKey => 'API key';

  @override
  String get detailResponse => 'Response time';

  @override
  String get detailFirstToken => 'First words after';

  @override
  String get detailTokens => 'Tokens';

  @override
  String get detailCost => 'Cost';

  @override
  String get detailFormat => 'Interface';

  @override
  String get detailTime => 'Time';

  @override
  String get detailRoute => 'Route';

  @override
  String get outcomeAnswered => 'answered';

  @override
  String get outcomeSlower => 'was slower – cancelled';

  @override
  String get outcomeBusy => 'was busy – skipped';

  @override
  String outcomeFailed(String reason) {
    return 'failed ($reason)';
  }

  @override
  String get tabConnected => 'Connected';

  @override
  String get tabLimits => 'Usage limits';

  @override
  String get accountFreePlan => 'Free plan';

  @override
  String get accountPaidPlan => 'Paid account';

  @override
  String accountPaidWithFree(String count) {
    return 'Paid account · $count free models';
  }

  @override
  String get accountFreeModelsOnly => 'Free models only';

  @override
  String get accountFreeVerified => 'Free plan, verified';

  @override
  String get accountBillingActive =>
      'Billing is active – nothing counts as free';

  @override
  String get accountNotVerified => 'Free plan, not verified yet';

  @override
  String get moreActions => 'More actions';

  @override
  String get chooseProviderHint =>
      'Pick the service you have a key for. Providers marked \"Free models\" can be used without paying.';

  @override
  String get providerFreeTag => 'Free models';

  @override
  String getKeyAt(String name) {
    return 'Get a key at $name';
  }

  @override
  String get billingOptionFree => 'Free tier';

  @override
  String get billingOptionFreeModels => 'Free models only';

  @override
  String get billingOptionFreeModelsHint =>
      'No credits on this account. NeoRouter uses only its free models and skips the rest.';

  @override
  String get billingOptionFreeHint =>
      'No payment method on file. NeoRouter only uses the free models.';

  @override
  String get billingOptionPlan => 'Prepaid plan';

  @override
  String get billingOptionPlanHint =>
      'A subscription with an included allowance. Used first while there is room; stops when it is used up.';

  @override
  String get billingOptionPlanPaid => 'Plan, then pay per use';

  @override
  String get billingOptionPlanPaidHint =>
      'The allowance first; after that requests are billed and count against key budgets.';

  @override
  String get billingOptionPaid => 'Pay per use';

  @override
  String get billingOptionPaidHint =>
      'Every request to a paid model is billed and counts against your API keys\' budgets.';

  @override
  String get accountPrepaidPlan => 'Prepaid plan';

  @override
  String get accountPlanThenPaid => 'Plan, then pay per use';

  @override
  String get includedInPlan => 'Included in plan';

  @override
  String get metricPlanRequests => 'Plan requests';

  @override
  String get metricPlan5h => 'Plan, 5-hour window';

  @override
  String get metricPlanWeek => 'Plan, this week';

  @override
  String get metricPlanMonth => 'Plan, this month';

  @override
  String get billingPaidHint =>
      'Requests to paid models are billed by the provider and count against your API keys\' budgets.';

  @override
  String get testConnection => 'Test connection';

  @override
  String probeOk(String count) {
    return 'The key works – $count models found.';
  }

  @override
  String get limitsDescription =>
      'What is left of each provider\'s allowance today. Per-minute limits reset on their own and NeoRouter routes around them, so only daily allowances and models running low are shown.';

  @override
  String limitResets(String time) {
    return 'resets $time';
  }

  @override
  String get limitConcurrent => 'Requests at once';

  @override
  String limitConcurrentValue(String count) {
    return 'up to $count, learned from its answers';
  }

  @override
  String limitConcurrentDocumented(String count) {
    return 'up to $count';
  }

  @override
  String get addAnotherAccount => 'Add another account';

  @override
  String limitsModelsRoom(String room, String total) {
    return '$room of $total models have room today';
  }

  @override
  String get filterProvider => 'Filter by connection';

  @override
  String get allConnections => 'All connections';

  @override
  String showMore(String count) {
    return 'Show more ($count left)';
  }

  @override
  String get factWhyFree => 'Free or paid';

  @override
  String get factContext => 'Context';

  @override
  String get factMeasured => 'Measured';

  @override
  String get keyAccessTitle => 'What can this key use?';

  @override
  String get keyBudgetTitle => 'Spending limit';

  @override
  String get keyNameHint => 'e.g. Laptop or Home Assistant';

  @override
  String get keySecretDone => 'Done';

  @override
  String keyUsed(String time) {
    return 'used $time';
  }

  @override
  String get keyNeverUsed => 'not used yet';

  @override
  String revokedKeys(String count) {
    return 'Revoked keys ($count)';
  }

  @override
  String get keysFooter =>
      'Each app should get its own key. If a key leaks, revoke it – your other apps keep working.';

  @override
  String get presetBalancedHint =>
      'A good mix of quality, speed, reliability and cost. Used when nothing else is set.';

  @override
  String get presetFastHint =>
      'The shortest wait for an answer, from models that rarely fail.';

  @override
  String get presetSmartHint =>
      'The most capable models, even if they take longer.';

  @override
  String get presetCheaperNote =>
      'Whatever the profile, of two models with similar quality the cheaper one is used.';

  @override
  String get autoKeyDefault => 'Auto (key default)';

  @override
  String get presetBalancedDefault => 'Balanced (default)';

  @override
  String get stylesTitle => 'Routing styles';

  @override
  String get stylesHint =>
      'Use a style by asking for its model name, e.g. \"auto:fast\", or choose one per API key.';

  @override
  String get customStylesTitle => 'Your own styles';

  @override
  String get customStylesHint =>
      'Decide yourself how much quality, speed, reliability and cost matter.';

  @override
  String get createStyle => 'New style';

  @override
  String get editStyle => 'Edit style';

  @override
  String get weightsTitle => 'What matters';

  @override
  String get previewDefaultPrompt => 'Explain quicksort in two sentences.';

  @override
  String get previewNone =>
      'No model fits right now. Connect a provider or turn off \"free models only\".';

  @override
  String previewSkipped(String count) {
    return '$count models skipped';
  }

  @override
  String get previewMostLikely => 'most likely';

  @override
  String get playgroundEmptyTitle => 'What would you like to try?';

  @override
  String get playgroundEmptyHint =>
      'Messages go through the real router with your key, so you see which model answers and how fast.';

  @override
  String get suggestion1 => 'Explain how a rainbow forms in two sentences';

  @override
  String get suggestion2 => 'Write a friendly reminder for a team meeting';

  @override
  String get suggestion3 => 'Give me three dinner ideas with chickpeas';

  @override
  String get newChat => 'New chat';

  @override
  String get modelPicker => 'Model';

  @override
  String get modelSpecific => 'Specific model…';

  @override
  String get modelSpecificHint => 'e.g. groq/openai/gpt-oss-120b';

  @override
  String get systemPromptTitle => 'Instructions';

  @override
  String get systemPromptHint =>
      'Optional: how the assistant should behave, e.g. \"Answer briefly and in German.\"';

  @override
  String get thinking => 'Thinking…';

  @override
  String get enterToSend => 'Enter to send · Shift+Enter for a new line';

  @override
  String get groupModels => 'Models';

  @override
  String get groupAccess => 'Access';

  @override
  String capComingSoonTitle(String kind) {
    return '$kind – coming soon';
  }

  @override
  String get capRouter => 'Other routers';

  @override
  String get capRouterHint =>
      'Services such as OpenRouter Auto or Jev Router that pick a model themselves. NeoRouter does the routing for you, so they are only listed.';

  @override
  String get metricSaved => 'Saved with free models';

  @override
  String metricSavedCaption(String spent, String percent) {
    return '$spent spent · $percent % free';
  }

  @override
  String get styleBuiltIn => 'Built-in styles';

  @override
  String get styleUseTitle => 'Use this style';

  @override
  String get styleUseHint =>
      'Ask for this model name in any app, or choose the style for an API key.';

  @override
  String get styleUseRow => 'Model name for your apps';

  @override
  String get styleWeightsHint =>
      'How much each quality counts when NeoRouter compares models. A model also has to be decent on everything that matters.';

  @override
  String get freeMode => 'Free only';

  @override
  String get freeModeOnHint =>
      'Only free models answer. Turn off to let NeoRouter also use paid models this key may use.';

  @override
  String get freeModeOffHint =>
      'Paid models may answer too, within this key\'s budget.';

  @override
  String get freeModeLocked => 'This API key only uses free models.';

  @override
  String get freeModeLockedExplain =>
      'This API key is set to \"Only free models\", so it always stays free. To allow paid models, pick another key or change this one under API keys.';

  @override
  String get navProcessing => 'Privacy & compression';

  @override
  String get processingTitle => 'Privacy & compression';

  @override
  String get processingDescription =>
      'What happens to every request before it leaves your server: hide private data from providers and make prompts smaller.';

  @override
  String get privacyShield => 'Privacy shield';

  @override
  String get compressionTitle => 'Compression';

  @override
  String get privacyHiddenValues => 'Values hidden';

  @override
  String get privacyProtectedRequests => 'Protected requests';

  @override
  String get last30Days => 'last 30 days';

  @override
  String ofRequests(String count) {
    return 'of $count requests';
  }

  @override
  String get privacyHowItWorks =>
      'Private values are swapped for placeholders like [EMAIL_1] before a request leaves your server. The AI only ever sees the placeholder, and NeoRouter puts the real value back into its answer.';

  @override
  String get privacyEnable => 'Hide private data';

  @override
  String get privacyEnableHint =>
      'Applies to every API key and the Playground. Embeddings, image and video prompts and web searches only get the values set to \"Hide only\".';

  @override
  String get privacyWhatToHide => 'What to hide';

  @override
  String get privacyModesHint =>
      '\"Hide & restore\" puts the value back into the answer. \"Hide only\" keeps it hidden for good – best for passwords and card numbers.';

  @override
  String get modeOff => 'Off';

  @override
  String get modeRestore => 'Hide & restore';

  @override
  String get modeHide => 'Hide only';

  @override
  String get catEmail => 'Email addresses';

  @override
  String get catEmailHint => 'anna@example.com';

  @override
  String get catPhone => 'Phone numbers';

  @override
  String get catPhoneHint => 'International and local formats.';

  @override
  String get catIban => 'Bank accounts';

  @override
  String get catIbanHint =>
      'IBAN (checked with its checksum), BIC, and account numbers named as such.';

  @override
  String get catCard => 'Payment cards';

  @override
  String get catCardHint =>
      'Real card numbers (issuer and checksum match), and security codes and expiry dates next to them.';

  @override
  String get catIp => 'Network addresses';

  @override
  String get catIpHint => 'IPv4, IPv6 and MAC addresses, except localhost.';

  @override
  String get catSecret => 'Passwords & API keys';

  @override
  String get catSecretHint =>
      'More than 250 key formats, private keys, \"password: …\", passwords in links, cookies, webhook links, one-time codes and wallet recovery phrases.';

  @override
  String get catBirthdate => 'Dates of birth';

  @override
  String get catBirthdateHint =>
      'Dates next to words like \"born\" or \"geb.\".';

  @override
  String get catCrypto => 'Crypto wallets';

  @override
  String get catCryptoHint =>
      'Bitcoin, Ethereum, Litecoin, Tron, Monero and Solana addresses.';

  @override
  String get catLocation => 'Locations';

  @override
  String get catLocationHint => 'GPS coordinates and Plus Codes.';

  @override
  String get catVehicle => 'Vehicles';

  @override
  String get catVehicleHint =>
      'Number plates and vehicle identification numbers.';

  @override
  String get catCustom => 'Your private terms';

  @override
  String get catCustomHint => 'The words you list below.';

  @override
  String get catPerson => 'Last names';

  @override
  String get catPersonHint =>
      'First names stay, last names are hidden: \"Anna [LASTNAME_1]\".';

  @override
  String get catAddress => 'Postal addresses';

  @override
  String get catAddressHint => 'Street and number, postcode and town.';

  @override
  String get catOrganization => 'Organizations';

  @override
  String get catOrganizationHint =>
      'Employers, clinics, schools – found by AI detection.';

  @override
  String get catId => 'ID, tax and insurance numbers';

  @override
  String get catIdHint =>
      'Steuer-ID, VAT IDs, ID cards, passports, social security and health insurance numbers from Germany, Europe, the UK and the US.';

  @override
  String get privacyTermsTitle => 'Your private terms';

  @override
  String get privacyTermsHint =>
      'Names, your company, your street – anything that should never reach a provider. One per line.';

  @override
  String get privacyTermsPlaceholder =>
      'Anna Schmidt\nAcme Holding\nHauptstraße 5';

  @override
  String get privacyAllowTitle => 'Never hide';

  @override
  String get privacyAllowHint =>
      'Values that may always go out, one per line. Start with @ to allow a whole mail domain, e.g. your company\'s.';

  @override
  String get privacyAllowPlaceholder => '@acme.example\nsupport@acme.example';

  @override
  String get privacyAiTitle => 'AI detection';

  @override
  String get privacyAiHint =>
      'Everything above is found on your server in about a millisecond, without AI. A fast AI model can also read each new message to find organizations and what the rules miss.';

  @override
  String get privacyAiEnable => 'Also look with AI';

  @override
  String get privacyAiEnableHint =>
      'Adds about a second to new messages; repeated messages are remembered.';

  @override
  String get privacyAiModel => 'Detection model';

  @override
  String get privacyAiModelHint =>
      '\"auto:fast\" picks the fastest model. Use a local model, e.g. \"ollama/…\", if no provider should see the raw text.';

  @override
  String get privacyAiFreeOnly => 'Use free models only for detection';

  @override
  String get privacyAiWarning =>
      'The detection model sees the original text of new messages. Pick a provider you trust or a local model.';

  @override
  String get privacyTryHint =>
      'Paste a text to see exactly what a provider would receive. Nothing is sent to a model (except AI detection, if it is on).';

  @override
  String get privacyTrySample =>
      'Hi, I am Anna Schmidt, born 14.07.1988, living at Lindenstraße 12, 50674 Köln. Please send the invoice to anna.schmidt@example.de and call me at +49 170 1234567. My IBAN is DE89 3704 0044 0532 0130 00, my tax ID 86 095 742 719, the server password: hunter2pw.';

  @override
  String privacyTryResult(String count) {
    return '$count values hidden';
  }

  @override
  String privacyTryFoundBy(String list) {
    return 'Found by: $list';
  }

  @override
  String get privacyFootnote =>
      'Hidden values never leave your server and are never written to disk; the mapping lives only for the duration of one request. Detection is a strong safety net, not a guarantee – keep very sensitive data out of prompts.';

  @override
  String get compressionSaved => 'Tokens saved';

  @override
  String get compressionRequests => 'Compressed requests';

  @override
  String get compressionLevel => 'Level';

  @override
  String get compressionLevelHint =>
      'Smaller prompts are cheaper, faster and fit more models. Higher levels save more but drop older details.';

  @override
  String get levelOff => 'Off';

  @override
  String get levelOffHint =>
      'Requests are sent exactly as your app wrote them.';

  @override
  String get levelLossless => 'Lossless';

  @override
  String get levelLosslessHint =>
      'Removes extra whitespace, minifies JSON and drops duplicate blocks. The meaning never changes; code is left untouched.';

  @override
  String get levelBalanced => 'Balanced';

  @override
  String get levelBalancedHint =>
      'Lossless, plus: repeated log lines are collapsed, JSON lists become compact tables and very long tool outputs keep their start and end. Recommended for agents.';

  @override
  String get levelAggressive => 'Aggressive';

  @override
  String get levelAggressiveHint =>
      'Balanced, plus: older tool outputs are hidden and older long messages are shortened. Research shows this keeps agents about as accurate at half the cost. Your latest message is never cut.';

  @override
  String get compressionEngines => 'Steps';

  @override
  String get compressionEnginesHint =>
      'Each step only applies when it actually makes the request smaller.';

  @override
  String get compressionLimits => 'When to compress';

  @override
  String get compressionMinTokens => 'Start lossy steps from (tokens)';

  @override
  String get compressionMinTokensHint =>
      'Shorter requests only get lossless clean-up.';

  @override
  String get compressionKeepRecent => 'Recent messages kept whole';

  @override
  String get compressionKeepRecentHint =>
      'Older messages may be shortened, these never.';

  @override
  String get compressionTarget => 'Token target (optional)';

  @override
  String get compressionTargetHint =>
      'If a request is still larger, the oldest messages are left out until it fits.';

  @override
  String get engWhitespace => 'Clean up whitespace';

  @override
  String get engWhitespaceHint =>
      'Trailing spaces and runs of empty lines, outside code.';

  @override
  String get engJsonMinify => 'Minify JSON';

  @override
  String get engJsonMinifyHint => 'Same data without indentation.';

  @override
  String get engDedupe => 'Drop duplicate blocks';

  @override
  String get engDedupeHint =>
      'Large texts sent twice are kept once (the newest).';

  @override
  String get engRepeatedLines => 'Collapse repeated lines';

  @override
  String get engRepeatedLinesHint =>
      '\"line repeated 40 more times\" instead of 40 identical log lines.';

  @override
  String get engJsonTables => 'JSON lists as tables';

  @override
  String get engJsonTablesHint =>
      'Lists of objects with the same fields list each field name once.';

  @override
  String get engTrimToolOutput => 'Trim long tool output';

  @override
  String get engTrimToolOutputHint =>
      'Keeps the beginning and end, where results and errors usually are.';

  @override
  String get engMaskOldToolOutput => 'Hide older tool output';

  @override
  String get engMaskOldToolOutputHint =>
      'Only the last three tool results stay complete.';

  @override
  String get engCondenseHistory => 'Shorten older messages';

  @override
  String get engCondenseHistoryHint =>
      'Long older messages keep their start and end.';

  @override
  String get engTokenTarget => 'Enforce the token target';

  @override
  String get engTokenTargetHint => 'Leaves out the oldest messages if needed.';

  @override
  String get compressionTryHint =>
      'Paste a long text or log to see how much the current level saves.';

  @override
  String compressionTryResult(String before, String after) {
    return '$before → $after tokens';
  }

  @override
  String get tryItTitle => 'Try it';

  @override
  String get tryItRun => 'Show result';

  @override
  String get tryItSent => 'What a provider would receive';

  @override
  String get pgEmbedHint =>
      'One text per line. NeoRouter turns each into a vector and shows how similar they are.';

  @override
  String get pgEmbedRun => 'Create vectors';

  @override
  String pgEmbedResult(String count, String dims) {
    return '$count vectors with $dims numbers each';
  }

  @override
  String get pgSimilarity => 'Similarity';

  @override
  String pgPinned(String model) {
    return 'This key now always uses $model for embeddings, so your vectors stay comparable.';
  }

  @override
  String get pgImageHint => 'Describe the picture you want.';

  @override
  String get pgImageRun => 'Create image';

  @override
  String get pgSize => 'Size';

  @override
  String get pgSpeechHint => 'Type what should be read aloud.';

  @override
  String get pgSpeechRun => 'Read aloud';

  @override
  String get pgVoice => 'Voice';

  @override
  String get pgVoiceDefault => 'Default voice';

  @override
  String get pgPlay => 'Play';

  @override
  String get pgStop => 'Stop';

  @override
  String get pgDownload => 'Download';

  @override
  String get pgTranscribeHint =>
      'Choose a recording (up to 25 MB) and NeoRouter turns it into text.';

  @override
  String get pgChooseFile => 'Choose audio file';

  @override
  String get pgUseSpeech => 'Use the spoken text';

  @override
  String get pgTranscribeRun => 'Transcribe';

  @override
  String get pgTranslate => 'Translate to English';

  @override
  String get pgCheckHint =>
      'Paste a message. A System One model decides in a split second whether it is safe.';

  @override
  String get pgCheckRun => 'Check';

  @override
  String get pgSafe => 'Looks safe';

  @override
  String get pgCheckSafety => 'Harmful content';

  @override
  String get pgCheckInjection => 'Prompt attacks';

  @override
  String get pgFlagged => 'Flagged';

  @override
  String get pgVideoHint =>
      'Describe a short scene. Videos take a few minutes and are never free.';

  @override
  String get pgVideoRun => 'Create video';

  @override
  String pgSeconds(String count) {
    return '$count seconds';
  }

  @override
  String pgVideoStatus(String status) {
    return 'Status: $status';
  }

  @override
  String get pgNoModels =>
      'None of your connections offers this kind of model yet.';

  @override
  String get pgAddProvider => 'Add a provider';

  @override
  String get unitPrice => 'Price per unit (USD)';

  @override
  String get unitImage => 'per image';

  @override
  String get unitSecond => 'per second';

  @override
  String get unitMinute => 'per minute of audio';

  @override
  String get unitMchar => 'per million characters';

  @override
  String get unitSearch => 'per search';

  @override
  String get pgSearchHint =>
      'Search the web through your search engines. Chat models use the same engines when a request turns web search on.';

  @override
  String get pgSearchRun => 'Search';

  @override
  String get pgNoResults => 'No results.';

  @override
  String get activityKind => 'Kind';

  @override
  String unitPriceLabel(String price, String unit) {
    return '$price $unit';
  }

  @override
  String get unitKind => 'Billed';

  @override
  String get navTeam => 'Team';

  @override
  String get teamTitle => 'Team';

  @override
  String get teamDescription =>
      'Let other people help manage your NeoRouter. You decide per person what they may see and change. Everything they create stays yours.';

  @override
  String get inviteSomeone => 'Invite someone';

  @override
  String get teamMembersTitle => 'People in your team';

  @override
  String get teamMembersHint =>
      'They work in your space with the rights you gave them.';

  @override
  String get noMembersTitle => 'Nobody here yet';

  @override
  String get noMembersMessage =>
      'Create an invite link and send it to the person. They need an account on this server.';

  @override
  String get inviteLinksTitle => 'Open invite links';

  @override
  String get inviteLinksHint =>
      'Anyone with an open link can join. Revoke links you no longer need.';

  @override
  String get joinTeamTitle => 'Someone else\'s team';

  @override
  String get joinTeamLabel => 'Got an invite link?';

  @override
  String get joinTeamHint =>
      'Join to help manage that person\'s NeoRouter. Your own space stays as it is.';

  @override
  String get useInviteLink => 'Use invite link';

  @override
  String memberOfTitle(String name) {
    return 'You are in $name\'s team';
  }

  @override
  String get memberOfHint => 'Switch spaces in the sidebar to work there.';

  @override
  String get youMay => 'You may';

  @override
  String openSpaceOf(String name) {
    return 'Open $name\'s space';
  }

  @override
  String get leaveTeam => 'Leave team';

  @override
  String leaveTeamConfirm(String name) {
    return 'Leave $name\'s team? You lose access until they invite you again. What you created there stays.';
  }

  @override
  String get editPermissions => 'Change rights';

  @override
  String permissionsFor(String name) {
    return 'What $name may do';
  }

  @override
  String get removeMember => 'Remove from team';

  @override
  String removeMemberConfirm(String name) {
    return 'Remove $name? They lose access right away. Keys and providers they created stay.';
  }

  @override
  String get noPermissions => 'Nothing yet';

  @override
  String permissionCount(String count) {
    return '$count rights';
  }

  @override
  String get permView => 'See';

  @override
  String get permCreate => 'Create';

  @override
  String get permEdit => 'Change';

  @override
  String get permDelete => 'Delete';

  @override
  String get permActivityView => 'See activity and costs';

  @override
  String get permPlaygroundUse => 'Use the playground';

  @override
  String get permProvidersManage => 'Add and change';

  @override
  String get permModelsManage => 'Turn models on or off';

  @override
  String get presetView => 'Only look';

  @override
  String get presetKeys => 'Manage API keys';

  @override
  String get presetFull => 'Everything';

  @override
  String get inviteLabel => 'Name';

  @override
  String get inviteLabelHint => 'Who is this link for?';

  @override
  String get inviteExpiry => 'Link works for';

  @override
  String get expiry24h => '24 hours';

  @override
  String get expiry7d => '7 days';

  @override
  String get expiry30d => '30 days';

  @override
  String get expiryNever => 'Until revoked';

  @override
  String get inviteSingleUseLabel => 'One person only';

  @override
  String get inviteSingleUseHint =>
      'The link stops working once someone joins.';

  @override
  String get invitePermissionsLabel => 'What they may do';

  @override
  String get createInviteLink => 'Create link';

  @override
  String get inviteLinkTitle => 'Invite link ready';

  @override
  String get inviteLinkBody =>
      'Send this link to the person. It is shown only now; you can revoke it any time.';

  @override
  String get inviteUntitled => 'Invite link';

  @override
  String get inviteSingleUse => 'One person';

  @override
  String inviteReusable(String count) {
    return 'Reusable · used $count×';
  }

  @override
  String get inviteNoExpiry => 'No expiry';

  @override
  String inviteExpires(String date) {
    return 'Until $date';
  }

  @override
  String get inviteLinkField => 'Invite link';

  @override
  String get inviteLinkFieldHint => 'Paste the link you got';

  @override
  String get inviteCheck => 'Check link';

  @override
  String invitePreview(String name) {
    return 'Join $name\'s team?';
  }

  @override
  String get invitePreviewHint =>
      'You can then work in their space with these rights:';

  @override
  String get joinTeam => 'Join team';

  @override
  String get spaceLabel => 'Space';

  @override
  String get mySpace => 'My space';

  @override
  String spaceOf(String name) {
    return '$name\'s space';
  }

  @override
  String get switchSpace => 'Switch space';

  @override
  String createdBy(String name) {
    return 'Created by $name';
  }

  @override
  String get formerMember => 'a former member';

  @override
  String inTeamOf(String name) {
    return 'in $name\'s team';
  }

  @override
  String get teamActivityTitle => 'Team activity';

  @override
  String get teamActivityHint =>
      'Invites, joins and changed rights across the server.';

  @override
  String get teamActivityEmpty => 'Nothing has happened yet.';

  @override
  String teamEventInviteCreate(String actor) {
    return '$actor created an invite link';
  }

  @override
  String teamEventInviteRevoke(String actor) {
    return '$actor revoked an invite link';
  }

  @override
  String teamEventJoin(String actor, String subject) {
    return '$actor joined $subject\'s team';
  }

  @override
  String teamEventPermissions(String actor, String subject) {
    return '$actor changed what $subject may do';
  }

  @override
  String teamEventRelease(String actor, String subject) {
    return '$actor removed $subject from the team';
  }

  @override
  String teamEventLeave(String actor, String subject) {
    return '$actor left $subject\'s team';
  }

  @override
  String get homeCapacityTitle => 'Around the clock';

  @override
  String homeCapacityValue(String count) {
    return '$count requests a day';
  }

  @override
  String homeCapacityCaption(String hour, String providers) {
    return 'What your free models could answer if used non-stop: about $hour an hour, from the usage limits of $providers providers.';
  }

  @override
  String homeCapacityUnknown(String models) {
    return '$models more free models publish no limit and are not counted.';
  }

  @override
  String get homeCapacityNone =>
      'None of your free models has a known usage limit yet, so there is nothing to add up.';

  @override
  String get keyLimitsTitle => 'Limits';

  @override
  String get keyNoLimitHint => 'Leave a field empty for no limit.';

  @override
  String get keyRateLimit => 'Rate limit';

  @override
  String get keyRequestsPerMinute => 'requests per minute';

  @override
  String get keyTokenLimit => 'Token limit';

  @override
  String get keyTokensPer => 'tokens per';

  @override
  String get keyTokenHint =>
      'Counts the tokens used on free and subscription models. Once reached, the key waits until it resets.';

  @override
  String get keyTokenFallsBack =>
      'Counts the tokens used on free and subscription models. Once reached, paid models take over within the budget.';

  @override
  String tokensUsedOf(String used, String limit) {
    return '$used of $limit tokens';
  }

  @override
  String get styleKindsTitle => 'Which models';

  @override
  String get styleKindsHint =>
      '\"If needed\" models step in only when the preferred ones cannot answer.';

  @override
  String get styleKindPrefer => 'Prefer';

  @override
  String get styleKindIfNeeded => 'If needed';

  @override
  String get styleKindNever => 'Never';

  @override
  String get stylePrioritiesTitle => 'What matters most';

  @override
  String get stylePriorityQuality => 'Quality';

  @override
  String get stylePriorityQualityHint => 'Smarter, more capable answers';

  @override
  String get stylePrioritySpeed => 'Speed';

  @override
  String get stylePrioritySpeedHint => 'Answers start sooner and finish faster';

  @override
  String get stylePriorityReliability => 'Reliability';

  @override
  String get stylePriorityReliabilityHint =>
      'Models that rarely fail or time out';

  @override
  String get stylePriorityCost => 'Saving money';

  @override
  String get stylePriorityCostHint => 'Cheaper models, free ones first';

  @override
  String get styleLevel0 => 'Not important';

  @override
  String get styleLevel1 => 'A little';

  @override
  String get styleLevel2 => 'Important';

  @override
  String get styleLevel3 => 'Very important';

  @override
  String get styleMoreTitle => 'Fine-tuning';

  @override
  String get styleQualityFloor => 'Lowest quality allowed';

  @override
  String get styleQualityAny => 'Any model';

  @override
  String get styleQualityDecent => 'Decent and up';

  @override
  String get styleQualityGood => 'Good and up';

  @override
  String get styleQualityTop => 'Top models only';

  @override
  String get stylePriceCap => 'Price limit for paid models';

  @override
  String get stylePriceAny => 'Any price';

  @override
  String get stylePricePremium => 'Up to premium models';

  @override
  String get stylePriceMid => 'Mid-priced and below';

  @override
  String get stylePriceCheap => 'Cheap models only';

  @override
  String get stylePriceVeryCheap => 'Very cheap only';

  @override
  String stylePriceDetail(String price) {
    return 'Answers cost at most $price per 750,000 words';
  }

  @override
  String get styleVariety => 'Variety';

  @override
  String get styleVarietyLow => 'Stick to the best model';

  @override
  String get styleVarietyMid => 'Spread over a few good models';

  @override
  String get styleVarietyHigh => 'Spread widely';

  @override
  String get styleAdaptive => 'Stronger models for hard requests';

  @override
  String get styleAdaptiveHint =>
      'Long, code-heavy or tool requests lean towards smarter models.';

  @override
  String get styleSkipProviders => 'Leave out';

  @override
  String get styleSkipProvidersHint => 'Connections this style never uses.';

  @override
  String get pgLiveHint =>
      'Live voice is a spoken conversation in real time over a WebSocket. Apps such as NeoAgent connect to the address for the protocol they speak, with a router key; NeoRouter picks the model and falls back if a provider refuses.';

  @override
  String get pgLiveEndpoints => 'Where apps connect';

  @override
  String get pgLiveModelHint =>
      'Use \"auto\" as the model, or one of the models below.';

  @override
  String get pgLiveModels => 'Live models';

  @override
  String get pgLiveNoModels =>
      'No live models yet. Gemini (AI Studio) and OpenAI connections bring them.';

  @override
  String connectTitle(String name) {
    return 'Connect $name?';
  }

  @override
  String connectSubtitle(String name) {
    return '$name wants its own key for your NeoRouter. You choose what it may use and can disconnect it any time under API keys.';
  }

  @override
  String connectSendsTo(String host) {
    return 'The key goes to $host';
  }

  @override
  String get connectInsecure =>
      'This address is not encrypted. Only continue if it is your own device or network.';

  @override
  String get connectDeviceHint =>
      'The app is waiting on another device and picks up the key by itself.';

  @override
  String get connectWantsToUse => 'It says it will use';

  @override
  String connectInSpace(String name) {
    return 'The key is created in $name.';
  }

  @override
  String get connectApprove => 'Connect';

  @override
  String get connectDeny => 'Decline';

  @override
  String get connectCodeTitle => 'Connect an app';

  @override
  String get connectCodeSubtitle => 'Enter the code the app shows you.';

  @override
  String get connectCodeField => 'Code';

  @override
  String get connectCodeContinue => 'Continue';

  @override
  String connectDoneTitle(String name) {
    return '$name is connected';
  }

  @override
  String get connectDoneBody =>
      'You can go back to the app. To disconnect it later, revoke its key under API keys.';

  @override
  String get connectDeniedTitle => 'Declined';

  @override
  String connectDeniedBody(String name) {
    return '$name did not get a key.';
  }

  @override
  String get connectFailedTitle => 'This connection did not work';

  @override
  String get connectOpenNeoRouter => 'Open NeoRouter';

  @override
  String get connectNoPermission =>
      'The team owner has not allowed you to create keys here. Switch to your own space to connect the app.';

  @override
  String get connectedAppsTitle => 'Connected apps';

  @override
  String get connectedAppsHint =>
      'Apps that signed in with NeoRouter. Revoke one to disconnect it.';

  @override
  String connectedVia(String host) {
    return 'Connected · $host';
  }

  @override
  String get connectedApp => 'Connected app';
}
