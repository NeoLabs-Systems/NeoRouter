import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_de.dart';
import 'app_localizations_en.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppL10n
/// returned by `AppL10n.of(context)`.
///
/// Applications need to include `AppL10n.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'gen/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppL10n.localizationsDelegates,
///   supportedLocales: AppL10n.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppL10n.supportedLocales
/// property.
abstract class AppL10n {
  AppL10n(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppL10n of(BuildContext context) {
    return Localizations.of<AppL10n>(context, AppL10n)!;
  }

  static const LocalizationsDelegate<AppL10n> delegate = _AppL10nDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[
    Locale('de'),
    Locale('en'),
  ];

  /// No description provided for @appName.
  ///
  /// In en, this message translates to:
  /// **'NeoRouter'**
  String get appName;

  /// No description provided for @brandTagline.
  ///
  /// In en, this message translates to:
  /// **'INFERENCE ROUTER'**
  String get brandTagline;

  /// No description provided for @cancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get cancel;

  /// No description provided for @save.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get save;

  /// No description provided for @delete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get delete;

  /// No description provided for @close.
  ///
  /// In en, this message translates to:
  /// **'Close'**
  String get close;

  /// No description provided for @create.
  ///
  /// In en, this message translates to:
  /// **'Create'**
  String get create;

  /// No description provided for @edit.
  ///
  /// In en, this message translates to:
  /// **'Edit'**
  String get edit;

  /// No description provided for @retry.
  ///
  /// In en, this message translates to:
  /// **'Try again'**
  String get retry;

  /// No description provided for @refresh.
  ///
  /// In en, this message translates to:
  /// **'Refresh'**
  String get refresh;

  /// No description provided for @copy.
  ///
  /// In en, this message translates to:
  /// **'Copy'**
  String get copy;

  /// No description provided for @copied.
  ///
  /// In en, this message translates to:
  /// **'Copied to clipboard.'**
  String get copied;

  /// No description provided for @saved.
  ///
  /// In en, this message translates to:
  /// **'Saved.'**
  String get saved;

  /// No description provided for @enabled.
  ///
  /// In en, this message translates to:
  /// **'Enabled'**
  String get enabled;

  /// No description provided for @disabled.
  ///
  /// In en, this message translates to:
  /// **'Disabled'**
  String get disabled;

  /// No description provided for @never.
  ///
  /// In en, this message translates to:
  /// **'never'**
  String get never;

  /// No description provided for @unlimited.
  ///
  /// In en, this message translates to:
  /// **'Unlimited'**
  String get unlimited;

  /// No description provided for @search.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get search;

  /// No description provided for @free.
  ///
  /// In en, this message translates to:
  /// **'Free'**
  String get free;

  /// No description provided for @paid.
  ///
  /// In en, this message translates to:
  /// **'Paid'**
  String get paid;

  /// No description provided for @unknown.
  ///
  /// In en, this message translates to:
  /// **'Unknown'**
  String get unknown;

  /// No description provided for @logout.
  ///
  /// In en, this message translates to:
  /// **'Sign out'**
  String get logout;

  /// No description provided for @serverUnreachable.
  ///
  /// In en, this message translates to:
  /// **'The server could not be reached.'**
  String get serverUnreachable;

  /// No description provided for @groupOverview.
  ///
  /// In en, this message translates to:
  /// **'Overview'**
  String get groupOverview;

  /// No description provided for @groupSetup.
  ///
  /// In en, this message translates to:
  /// **'Setup'**
  String get groupSetup;

  /// No description provided for @groupRouting.
  ///
  /// In en, this message translates to:
  /// **'Routing'**
  String get groupRouting;

  /// No description provided for @navDashboard.
  ///
  /// In en, this message translates to:
  /// **'Dashboard'**
  String get navDashboard;

  /// No description provided for @navProviders.
  ///
  /// In en, this message translates to:
  /// **'Providers'**
  String get navProviders;

  /// No description provided for @navModels.
  ///
  /// In en, this message translates to:
  /// **'Models'**
  String get navModels;

  /// No description provided for @navKeys.
  ///
  /// In en, this message translates to:
  /// **'Router keys'**
  String get navKeys;

  /// No description provided for @navRouting.
  ///
  /// In en, this message translates to:
  /// **'Auto router'**
  String get navRouting;

  /// No description provided for @navQuotas.
  ///
  /// In en, this message translates to:
  /// **'Free quotas'**
  String get navQuotas;

  /// No description provided for @navLogs.
  ///
  /// In en, this message translates to:
  /// **'Request log'**
  String get navLogs;

  /// No description provided for @navPlayground.
  ///
  /// In en, this message translates to:
  /// **'Playground'**
  String get navPlayground;

  /// No description provided for @navSettings.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get navSettings;

  /// No description provided for @authWelcome.
  ///
  /// In en, this message translates to:
  /// **'Welcome back'**
  String get authWelcome;

  /// No description provided for @authCreate.
  ///
  /// In en, this message translates to:
  /// **'Create your account'**
  String get authCreate;

  /// No description provided for @authBootstrapTitle.
  ///
  /// In en, this message translates to:
  /// **'Set up NeoRouter'**
  String get authBootstrapTitle;

  /// No description provided for @authLoginSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Sign in to continue.'**
  String get authLoginSubtitle;

  /// No description provided for @authRegisterSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Self-hosted. Your provider keys stay on your server.'**
  String get authRegisterSubtitle;

  /// No description provided for @authBootstrapSubtitle.
  ///
  /// In en, this message translates to:
  /// **'The first account becomes the administrator.'**
  String get authBootstrapSubtitle;

  /// No description provided for @username.
  ///
  /// In en, this message translates to:
  /// **'Username'**
  String get username;

  /// No description provided for @usernameOrEmail.
  ///
  /// In en, this message translates to:
  /// **'Username or email'**
  String get usernameOrEmail;

  /// No description provided for @email.
  ///
  /// In en, this message translates to:
  /// **'Email'**
  String get email;

  /// No description provided for @password.
  ///
  /// In en, this message translates to:
  /// **'Password'**
  String get password;

  /// No description provided for @confirmPassword.
  ///
  /// In en, this message translates to:
  /// **'Confirm password'**
  String get confirmPassword;

  /// No description provided for @signIn.
  ///
  /// In en, this message translates to:
  /// **'Sign in'**
  String get signIn;

  /// No description provided for @createAccount.
  ///
  /// In en, this message translates to:
  /// **'Create account'**
  String get createAccount;

  /// No description provided for @toRegister.
  ///
  /// In en, this message translates to:
  /// **'Don\'t have an account? Register'**
  String get toRegister;

  /// No description provided for @toLogin.
  ///
  /// In en, this message translates to:
  /// **'Already have an account? Sign in'**
  String get toLogin;

  /// No description provided for @forgotPassword.
  ///
  /// In en, this message translates to:
  /// **'Forgot password?'**
  String get forgotPassword;

  /// No description provided for @forgotPasswordTitle.
  ///
  /// In en, this message translates to:
  /// **'Reset password'**
  String get forgotPasswordTitle;

  /// No description provided for @forgotPasswordBody.
  ///
  /// In en, this message translates to:
  /// **'An administrator can reset it on the server with: neorouter reset-password <user> <new-password>'**
  String get forgotPasswordBody;

  /// No description provided for @passwordsDontMatch.
  ///
  /// In en, this message translates to:
  /// **'Passwords do not match.'**
  String get passwordsDontMatch;

  /// No description provided for @passwordTooShort.
  ///
  /// In en, this message translates to:
  /// **'Use a password with at least 12 characters.'**
  String get passwordTooShort;

  /// No description provided for @registrationClosed.
  ///
  /// In en, this message translates to:
  /// **'Registration is closed. Ask an administrator to open it.'**
  String get registrationClosed;

  /// No description provided for @dashboardEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Overview'**
  String get dashboardEyebrow;

  /// No description provided for @dashboardTitle.
  ///
  /// In en, this message translates to:
  /// **'Dashboard'**
  String get dashboardTitle;

  /// No description provided for @dashboardDescription.
  ///
  /// In en, this message translates to:
  /// **'Traffic, cost and health across every provider you connected.'**
  String get dashboardDescription;

  /// No description provided for @range24h.
  ///
  /// In en, this message translates to:
  /// **'24 h'**
  String get range24h;

  /// No description provided for @range7d.
  ///
  /// In en, this message translates to:
  /// **'7 days'**
  String get range7d;

  /// No description provided for @range30d.
  ///
  /// In en, this message translates to:
  /// **'30 days'**
  String get range30d;

  /// No description provided for @statRequests.
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get statRequests;

  /// No description provided for @statSuccessRate.
  ///
  /// In en, this message translates to:
  /// **'Success rate'**
  String get statSuccessRate;

  /// No description provided for @statCost.
  ///
  /// In en, this message translates to:
  /// **'Spent'**
  String get statCost;

  /// No description provided for @statTokens.
  ///
  /// In en, this message translates to:
  /// **'Tokens'**
  String get statTokens;

  /// No description provided for @statFreeShare.
  ///
  /// In en, this message translates to:
  /// **'Served free'**
  String get statFreeShare;

  /// No description provided for @statLatency.
  ///
  /// In en, this message translates to:
  /// **'Avg. latency'**
  String get statLatency;

  /// No description provided for @statFallbacks.
  ///
  /// In en, this message translates to:
  /// **'With fallback'**
  String get statFallbacks;

  /// No description provided for @statInOut.
  ///
  /// In en, this message translates to:
  /// **'{input} in · {output} out'**
  String statInOut(String input, String output);

  /// No description provided for @topModels.
  ///
  /// In en, this message translates to:
  /// **'Top models'**
  String get topModels;

  /// No description provided for @requestsShort.
  ///
  /// In en, this message translates to:
  /// **'{count} req.'**
  String requestsShort(String count);

  /// No description provided for @trafficTitle.
  ///
  /// In en, this message translates to:
  /// **'Traffic'**
  String get trafficTitle;

  /// No description provided for @errorsTitle.
  ///
  /// In en, this message translates to:
  /// **'Errors'**
  String get errorsTitle;

  /// No description provided for @noTrafficTitle.
  ///
  /// In en, this message translates to:
  /// **'No traffic yet'**
  String get noTrafficTitle;

  /// No description provided for @noTrafficMessage.
  ///
  /// In en, this message translates to:
  /// **'Send a request to your endpoint and it shows up here.'**
  String get noTrafficMessage;

  /// No description provided for @endpointTitle.
  ///
  /// In en, this message translates to:
  /// **'Your endpoint'**
  String get endpointTitle;

  /// No description provided for @endpointHint.
  ///
  /// In en, this message translates to:
  /// **'OpenAI-, Anthropic- and Responses-compatible. Authenticate with a router key.'**
  String get endpointHint;

  /// No description provided for @quickStartTitle.
  ///
  /// In en, this message translates to:
  /// **'Get started'**
  String get quickStartTitle;

  /// No description provided for @quickStartProviders.
  ///
  /// In en, this message translates to:
  /// **'Add a provider – the key is checked and all models are imported.'**
  String get quickStartProviders;

  /// No description provided for @quickStartKeys.
  ///
  /// In en, this message translates to:
  /// **'Create a router key with a budget or free-only mode.'**
  String get quickStartKeys;

  /// No description provided for @quickStartUse.
  ///
  /// In en, this message translates to:
  /// **'Use model \"auto\" or \"provider/model\" from any OpenAI or Anthropic SDK.'**
  String get quickStartUse;

  /// No description provided for @homeAllGood.
  ///
  /// In en, this message translates to:
  /// **'Everything is routing smoothly'**
  String get homeAllGood;

  /// No description provided for @homeNeedsAttention.
  ///
  /// In en, this message translates to:
  /// **'A provider needs your attention'**
  String get homeNeedsAttention;

  /// No description provided for @homeHealthyOf.
  ///
  /// In en, this message translates to:
  /// **'{healthy} of {total} providers healthy'**
  String homeHealthyOf(String healthy, String total);

  /// No description provided for @homeOpenPlayground.
  ///
  /// In en, this message translates to:
  /// **'Open Playground'**
  String get homeOpenPlayground;

  /// No description provided for @homeProblemHint.
  ///
  /// In en, this message translates to:
  /// **'Requests skip it until this is fixed. Nothing else is affected.'**
  String get homeProblemHint;

  /// No description provided for @homeFix.
  ///
  /// In en, this message translates to:
  /// **'Fix'**
  String get homeFix;

  /// No description provided for @homeConnectTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect your app'**
  String get homeConnectTitle;

  /// No description provided for @homeConnectHint.
  ///
  /// In en, this message translates to:
  /// **'Works with any OpenAI or Anthropic SDK. Ask for \"auto\" and NeoRouter picks the model.'**
  String get homeConnectHint;

  /// No description provided for @homeKeyHint.
  ///
  /// In en, this message translates to:
  /// **'Use a router key, e.g. {name} ({prefix}…).'**
  String homeKeyHint(String name, String prefix);

  /// No description provided for @homeManage.
  ///
  /// In en, this message translates to:
  /// **'Manage'**
  String get homeManage;

  /// No description provided for @homeRecent.
  ///
  /// In en, this message translates to:
  /// **'Recent requests'**
  String get homeRecent;

  /// No description provided for @homeAllActivity.
  ///
  /// In en, this message translates to:
  /// **'All activity'**
  String get homeAllActivity;

  /// No description provided for @homeNoRequests.
  ///
  /// In en, this message translates to:
  /// **'No requests yet. Send one from the Playground or your app.'**
  String get homeNoRequests;

  /// No description provided for @homeQuotaLeft.
  ///
  /// In en, this message translates to:
  /// **'{percent} % quota left'**
  String homeQuotaLeft(String percent);

  /// No description provided for @homeResetsIn.
  ///
  /// In en, this message translates to:
  /// **'resets in {time}'**
  String homeResetsIn(String time);

  /// No description provided for @homeOk.
  ///
  /// In en, this message translates to:
  /// **'OK'**
  String get homeOk;

  /// No description provided for @homeRetried.
  ///
  /// In en, this message translates to:
  /// **'Retried'**
  String get homeRetried;

  /// No description provided for @providersEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Setup'**
  String get providersEyebrow;

  /// No description provided for @providersTitle.
  ///
  /// In en, this message translates to:
  /// **'Providers'**
  String get providersTitle;

  /// No description provided for @providersDescription.
  ///
  /// In en, this message translates to:
  /// **'Connect any inference API. Keys are verified, encrypted and their models imported automatically.'**
  String get providersDescription;

  /// No description provided for @addProvider.
  ///
  /// In en, this message translates to:
  /// **'Add provider'**
  String get addProvider;

  /// No description provided for @chooseProvider.
  ///
  /// In en, this message translates to:
  /// **'Choose a provider'**
  String get chooseProvider;

  /// No description provided for @searchProviders.
  ///
  /// In en, this message translates to:
  /// **'Search providers…'**
  String get searchProviders;

  /// No description provided for @categoryFree.
  ///
  /// In en, this message translates to:
  /// **'Free tiers'**
  String get categoryFree;

  /// No description provided for @categoryAggregator.
  ///
  /// In en, this message translates to:
  /// **'Aggregators'**
  String get categoryAggregator;

  /// No description provided for @categoryPaid.
  ///
  /// In en, this message translates to:
  /// **'Paid APIs'**
  String get categoryPaid;

  /// No description provided for @categoryLocal.
  ///
  /// In en, this message translates to:
  /// **'Local'**
  String get categoryLocal;

  /// No description provided for @categoryCustom.
  ///
  /// In en, this message translates to:
  /// **'Custom'**
  String get categoryCustom;

  /// No description provided for @categoryCommunity.
  ///
  /// In en, this message translates to:
  /// **'Community'**
  String get categoryCommunity;

  /// No description provided for @apiKey.
  ///
  /// In en, this message translates to:
  /// **'API key'**
  String get apiKey;

  /// No description provided for @apiKeyOptional.
  ///
  /// In en, this message translates to:
  /// **'API key (optional)'**
  String get apiKeyOptional;

  /// No description provided for @apiKeyKeep.
  ///
  /// In en, this message translates to:
  /// **'API key (leave empty to keep the current one)'**
  String get apiKeyKeep;

  /// No description provided for @baseUrl.
  ///
  /// In en, this message translates to:
  /// **'Base URL'**
  String get baseUrl;

  /// No description provided for @displayNameLabel.
  ///
  /// In en, this message translates to:
  /// **'Display name'**
  String get displayNameLabel;

  /// No description provided for @slugLabel.
  ///
  /// In en, this message translates to:
  /// **'Name in model ids'**
  String get slugLabel;

  /// No description provided for @slugHint.
  ///
  /// In en, this message translates to:
  /// **'Models are addressed as {slug}/<model>.'**
  String slugHint(String slug);

  /// No description provided for @billingMode.
  ///
  /// In en, this message translates to:
  /// **'Billing'**
  String get billingMode;

  /// No description provided for @billingFree.
  ///
  /// In en, this message translates to:
  /// **'Free tier – no payment method'**
  String get billingFree;

  /// No description provided for @billingPaid.
  ///
  /// In en, this message translates to:
  /// **'Paid account'**
  String get billingPaid;

  /// No description provided for @billingFreeWarning.
  ///
  /// In en, this message translates to:
  /// **'Only choose the free tier if this account cannot be charged. NeoRouter then treats every model of this account as 100% free.'**
  String get billingFreeWarning;

  /// No description provided for @billingAlwaysFreeNote.
  ///
  /// In en, this message translates to:
  /// **'Some models of this provider are always free, independent of billing.'**
  String get billingAlwaysFreeNote;

  /// No description provided for @getKey.
  ///
  /// In en, this message translates to:
  /// **'Get a key'**
  String get getKey;

  /// No description provided for @checkAndAdd.
  ///
  /// In en, this message translates to:
  /// **'Check key & import models'**
  String get checkAndAdd;

  /// No description provided for @providerAdded.
  ///
  /// In en, this message translates to:
  /// **'Key verified – {count} models imported.'**
  String providerAdded(String count);

  /// No description provided for @providerAddedUnverified.
  ///
  /// In en, this message translates to:
  /// **'Saved. The key could not be verified yet: {message}'**
  String providerAddedUnverified(String message);

  /// No description provided for @statusHealthy.
  ///
  /// In en, this message translates to:
  /// **'Healthy'**
  String get statusHealthy;

  /// No description provided for @statusInvalid.
  ///
  /// In en, this message translates to:
  /// **'Key rejected'**
  String get statusInvalid;

  /// No description provided for @statusError.
  ///
  /// In en, this message translates to:
  /// **'Check failed'**
  String get statusError;

  /// No description provided for @statusUnknown.
  ///
  /// In en, this message translates to:
  /// **'Not checked'**
  String get statusUnknown;

  /// No description provided for @statusDisabled.
  ///
  /// In en, this message translates to:
  /// **'Disabled'**
  String get statusDisabled;

  /// No description provided for @modelsCount.
  ///
  /// In en, this message translates to:
  /// **'{total} models · {free} free'**
  String modelsCount(String total, String free);

  /// No description provided for @revalidate.
  ///
  /// In en, this message translates to:
  /// **'Check key'**
  String get revalidate;

  /// No description provided for @syncModels.
  ///
  /// In en, this message translates to:
  /// **'Refresh models'**
  String get syncModels;

  /// No description provided for @probeQuota.
  ///
  /// In en, this message translates to:
  /// **'Check balance'**
  String get probeQuota;

  /// No description provided for @removeProvider.
  ///
  /// In en, this message translates to:
  /// **'Remove provider'**
  String get removeProvider;

  /// No description provided for @removeProviderConfirm.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}? Its models disappear from routing; request history is kept.'**
  String removeProviderConfirm(String name);

  /// No description provided for @editProvider.
  ///
  /// In en, this message translates to:
  /// **'Edit provider'**
  String get editProvider;

  /// No description provided for @lastChecked.
  ///
  /// In en, this message translates to:
  /// **'Checked {time}'**
  String lastChecked(String time);

  /// No description provided for @syncedModels.
  ///
  /// In en, this message translates to:
  /// **'{count} models synced.'**
  String syncedModels(String count);

  /// No description provided for @noProvidersTitle.
  ///
  /// In en, this message translates to:
  /// **'No providers yet'**
  String get noProvidersTitle;

  /// No description provided for @noProvidersMessage.
  ///
  /// In en, this message translates to:
  /// **'Add your first provider to fill the model catalog.'**
  String get noProvidersMessage;

  /// No description provided for @quotaProbeResult.
  ///
  /// In en, this message translates to:
  /// **'{metric}: {remaining}'**
  String quotaProbeResult(String metric, String remaining);

  /// No description provided for @quotaProbeEmpty.
  ///
  /// In en, this message translates to:
  /// **'This provider does not report a balance.'**
  String get quotaProbeEmpty;

  /// No description provided for @modelsEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Catalog'**
  String get modelsEyebrow;

  /// No description provided for @modelsTitle.
  ///
  /// In en, this message translates to:
  /// **'Models'**
  String get modelsTitle;

  /// No description provided for @modelsDescription.
  ///
  /// In en, this message translates to:
  /// **'Every chat model your providers expose, with learned speed and reliability.'**
  String get modelsDescription;

  /// No description provided for @searchModels.
  ///
  /// In en, this message translates to:
  /// **'Search models…'**
  String get searchModels;

  /// No description provided for @filterAll.
  ///
  /// In en, this message translates to:
  /// **'All'**
  String get filterAll;

  /// No description provided for @filterFree.
  ///
  /// In en, this message translates to:
  /// **'Free'**
  String get filterFree;

  /// No description provided for @filterEnabled.
  ///
  /// In en, this message translates to:
  /// **'Enabled'**
  String get filterEnabled;

  /// No description provided for @filterTools.
  ///
  /// In en, this message translates to:
  /// **'Tools'**
  String get filterTools;

  /// No description provided for @filterVision.
  ///
  /// In en, this message translates to:
  /// **'Vision'**
  String get filterVision;

  /// No description provided for @filterIssues.
  ///
  /// In en, this message translates to:
  /// **'Issues'**
  String get filterIssues;

  /// No description provided for @intelligence.
  ///
  /// In en, this message translates to:
  /// **'Intelligence'**
  String get intelligence;

  /// No description provided for @speed.
  ///
  /// In en, this message translates to:
  /// **'Speed'**
  String get speed;

  /// No description provided for @reliability.
  ///
  /// In en, this message translates to:
  /// **'Reliability'**
  String get reliability;

  /// No description provided for @cost.
  ///
  /// In en, this message translates to:
  /// **'Cost'**
  String get cost;

  /// No description provided for @price.
  ///
  /// In en, this message translates to:
  /// **'Price'**
  String get price;

  /// No description provided for @pricePerMillion.
  ///
  /// In en, this message translates to:
  /// **'{input} / {output} per 1M'**
  String pricePerMillion(String input, String output);

  /// No description provided for @priceUnknown.
  ///
  /// In en, this message translates to:
  /// **'Price unknown'**
  String get priceUnknown;

  /// No description provided for @contextWindow.
  ///
  /// In en, this message translates to:
  /// **'{tokens} context'**
  String contextWindow(String tokens);

  /// No description provided for @tools.
  ///
  /// In en, this message translates to:
  /// **'Tools'**
  String get tools;

  /// No description provided for @vision.
  ///
  /// In en, this message translates to:
  /// **'Vision'**
  String get vision;

  /// No description provided for @reasoning.
  ///
  /// In en, this message translates to:
  /// **'Reasoning'**
  String get reasoning;

  /// No description provided for @freeReasonManualFree.
  ///
  /// In en, this message translates to:
  /// **'Marked free by you'**
  String get freeReasonManualFree;

  /// No description provided for @freeReasonManualPaid.
  ///
  /// In en, this message translates to:
  /// **'Marked paid by you'**
  String get freeReasonManualPaid;

  /// No description provided for @freeReasonProviderFreeModel.
  ///
  /// In en, this message translates to:
  /// **'Free model of this provider'**
  String get freeReasonProviderFreeModel;

  /// No description provided for @freeReasonListedZeroPrice.
  ///
  /// In en, this message translates to:
  /// **'Provider lists a price of 0'**
  String get freeReasonListedZeroPrice;

  /// No description provided for @freeReasonFreeTierAccount.
  ///
  /// In en, this message translates to:
  /// **'Free-tier account'**
  String get freeReasonFreeTierAccount;

  /// No description provided for @freeReasonPaid.
  ///
  /// In en, this message translates to:
  /// **'Paid'**
  String get freeReasonPaid;

  /// No description provided for @freeReasonPriceUnknown.
  ///
  /// In en, this message translates to:
  /// **'Price unknown – not counted as free'**
  String get freeReasonPriceUnknown;

  /// No description provided for @modelSettings.
  ///
  /// In en, this message translates to:
  /// **'Model settings'**
  String get modelSettings;

  /// No description provided for @overrideFree.
  ///
  /// In en, this message translates to:
  /// **'Free status'**
  String get overrideFree;

  /// No description provided for @overrideAuto.
  ///
  /// In en, this message translates to:
  /// **'Automatic'**
  String get overrideAuto;

  /// No description provided for @overrideForceFree.
  ///
  /// In en, this message translates to:
  /// **'Always free'**
  String get overrideForceFree;

  /// No description provided for @overrideForcePaid.
  ///
  /// In en, this message translates to:
  /// **'Always paid'**
  String get overrideForcePaid;

  /// No description provided for @inputPrice.
  ///
  /// In en, this message translates to:
  /// **'Input USD / 1M'**
  String get inputPrice;

  /// No description provided for @outputPrice.
  ///
  /// In en, this message translates to:
  /// **'Output USD / 1M'**
  String get outputPrice;

  /// No description provided for @intelligenceOverride.
  ///
  /// In en, this message translates to:
  /// **'Intelligence override (0–1, empty = automatic)'**
  String get intelligenceOverride;

  /// No description provided for @intelligenceSource.
  ///
  /// In en, this message translates to:
  /// **'Source: {source}'**
  String intelligenceSource(String source);

  /// No description provided for @resetHealth.
  ///
  /// In en, this message translates to:
  /// **'Reset health'**
  String get resetHealth;

  /// No description provided for @healthReset.
  ///
  /// In en, this message translates to:
  /// **'Penalties and cooldowns cleared.'**
  String get healthReset;

  /// No description provided for @enableAll.
  ///
  /// In en, this message translates to:
  /// **'Enable all'**
  String get enableAll;

  /// No description provided for @disableAll.
  ///
  /// In en, this message translates to:
  /// **'Disable all'**
  String get disableAll;

  /// No description provided for @enableFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Enable only free'**
  String get enableFreeOnly;

  /// No description provided for @measured.
  ///
  /// In en, this message translates to:
  /// **'{latency} · {tps} tok/s · {samples} calls'**
  String measured(String latency, String tps, String samples);

  /// No description provided for @notMeasured.
  ///
  /// In en, this message translates to:
  /// **'Not measured yet'**
  String get notMeasured;

  /// No description provided for @penaltyLabel.
  ///
  /// In en, this message translates to:
  /// **'Penalty {value}'**
  String penaltyLabel(String value);

  /// No description provided for @unavailableUntil.
  ///
  /// In en, this message translates to:
  /// **'{reason} until {time}'**
  String unavailableUntil(String reason, String time);

  /// No description provided for @noModelsTitle.
  ///
  /// In en, this message translates to:
  /// **'No models'**
  String get noModelsTitle;

  /// No description provided for @noModelsMessage.
  ///
  /// In en, this message translates to:
  /// **'Add a provider or change the filter.'**
  String get noModelsMessage;

  /// No description provided for @modelsShown.
  ///
  /// In en, this message translates to:
  /// **'{count} models'**
  String modelsShown(String count);

  /// No description provided for @keysEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Access'**
  String get keysEyebrow;

  /// No description provided for @keysTitle.
  ///
  /// In en, this message translates to:
  /// **'Router keys'**
  String get keysTitle;

  /// No description provided for @keysDescription.
  ///
  /// In en, this message translates to:
  /// **'Keys for your apps. Each key has its own mode, budget and limits.'**
  String get keysDescription;

  /// No description provided for @createKey.
  ///
  /// In en, this message translates to:
  /// **'Create key'**
  String get createKey;

  /// No description provided for @editKey.
  ///
  /// In en, this message translates to:
  /// **'Edit key'**
  String get editKey;

  /// No description provided for @keyName.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get keyName;

  /// No description provided for @keyMode.
  ///
  /// In en, this message translates to:
  /// **'Model access'**
  String get keyMode;

  /// No description provided for @modeFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Only 100% free models'**
  String get modeFreeOnly;

  /// No description provided for @modePaid.
  ///
  /// In en, this message translates to:
  /// **'All models, stop at the budget'**
  String get modePaid;

  /// No description provided for @modeBoth.
  ///
  /// In en, this message translates to:
  /// **'All models, then free ones once the budget is used'**
  String get modeBoth;

  /// No description provided for @budget.
  ///
  /// In en, this message translates to:
  /// **'Budget (USD, empty = no limit)'**
  String get budget;

  /// No description provided for @budgetPeriod.
  ///
  /// In en, this message translates to:
  /// **'Budget period'**
  String get budgetPeriod;

  /// No description provided for @periodTotal.
  ///
  /// In en, this message translates to:
  /// **'Total'**
  String get periodTotal;

  /// No description provided for @periodMonthly.
  ///
  /// In en, this message translates to:
  /// **'Per month'**
  String get periodMonthly;

  /// No description provided for @periodDaily.
  ///
  /// In en, this message translates to:
  /// **'Per day'**
  String get periodDaily;

  /// No description provided for @rpmLimit.
  ///
  /// In en, this message translates to:
  /// **'Requests per minute (empty = no limit)'**
  String get rpmLimit;

  /// No description provided for @allowedModels.
  ///
  /// In en, this message translates to:
  /// **'Allowed models – glob patterns, one per line (empty = all)'**
  String get allowedModels;

  /// No description provided for @defaultProfile.
  ///
  /// In en, this message translates to:
  /// **'Routing profile for \"auto\"'**
  String get defaultProfile;

  /// No description provided for @keySecretTitle.
  ///
  /// In en, this message translates to:
  /// **'Your new router key'**
  String get keySecretTitle;

  /// No description provided for @keySecretBody.
  ///
  /// In en, this message translates to:
  /// **'Copy it now – it is stored only as a hash and cannot be shown again.'**
  String get keySecretBody;

  /// No description provided for @revoke.
  ///
  /// In en, this message translates to:
  /// **'Revoke'**
  String get revoke;

  /// No description provided for @revokeConfirm.
  ///
  /// In en, this message translates to:
  /// **'Revoke {name}? Apps using it stop working immediately.'**
  String revokeConfirm(String name);

  /// No description provided for @revoked.
  ///
  /// In en, this message translates to:
  /// **'Revoked'**
  String get revoked;

  /// No description provided for @spentOf.
  ///
  /// In en, this message translates to:
  /// **'{spent} of {budget}'**
  String spentOf(String spent, String budget);

  /// No description provided for @spentNoBudget.
  ///
  /// In en, this message translates to:
  /// **'{spent} spent'**
  String spentNoBudget(String spent);

  /// No description provided for @resetsAt.
  ///
  /// In en, this message translates to:
  /// **'resets {time}'**
  String resetsAt(String time);

  /// No description provided for @lastUsed.
  ///
  /// In en, this message translates to:
  /// **'Last used {time}'**
  String lastUsed(String time);

  /// No description provided for @noKeysTitle.
  ///
  /// In en, this message translates to:
  /// **'No router keys'**
  String get noKeysTitle;

  /// No description provided for @noKeysMessage.
  ///
  /// In en, this message translates to:
  /// **'Create a key to call your endpoint.'**
  String get noKeysMessage;

  /// No description provided for @deleteKeyConfirm.
  ///
  /// In en, this message translates to:
  /// **'Delete {name} permanently?'**
  String deleteKeyConfirm(String name);

  /// No description provided for @routingEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Auto router'**
  String get routingEyebrow;

  /// No description provided for @routingTitle.
  ///
  /// In en, this message translates to:
  /// **'Routing'**
  String get routingTitle;

  /// No description provided for @routingDescription.
  ///
  /// In en, this message translates to:
  /// **'How \"auto\" picks a model. Scores combine intelligence, measured speed, learned reliability and price; failing or slow models are penalized and load is spread.'**
  String get routingDescription;

  /// No description provided for @presetBalanced.
  ///
  /// In en, this message translates to:
  /// **'Balanced'**
  String get presetBalanced;

  /// No description provided for @presetReliable.
  ///
  /// In en, this message translates to:
  /// **'Most reliable'**
  String get presetReliable;

  /// No description provided for @presetFast.
  ///
  /// In en, this message translates to:
  /// **'Fastest'**
  String get presetFast;

  /// No description provided for @presetSmart.
  ///
  /// In en, this message translates to:
  /// **'Smartest'**
  String get presetSmart;

  /// No description provided for @presetBest.
  ///
  /// In en, this message translates to:
  /// **'Best'**
  String get presetBest;

  /// No description provided for @presetCheap.
  ///
  /// In en, this message translates to:
  /// **'Cheapest'**
  String get presetCheap;

  /// No description provided for @presetCustom.
  ///
  /// In en, this message translates to:
  /// **'Custom'**
  String get presetCustom;

  /// No description provided for @presetSlider.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get presetSlider;

  /// No description provided for @weights.
  ///
  /// In en, this message translates to:
  /// **'Weights'**
  String get weights;

  /// No description provided for @constraints.
  ///
  /// In en, this message translates to:
  /// **'Constraints'**
  String get constraints;

  /// No description provided for @customProfiles.
  ///
  /// In en, this message translates to:
  /// **'Custom profiles'**
  String get customProfiles;

  /// No description provided for @profileName.
  ///
  /// In en, this message translates to:
  /// **'Profile name'**
  String get profileName;

  /// No description provided for @saveProfile.
  ///
  /// In en, this message translates to:
  /// **'Save as profile'**
  String get saveProfile;

  /// No description provided for @updateProfile.
  ///
  /// In en, this message translates to:
  /// **'Update profile'**
  String get updateProfile;

  /// No description provided for @profileUsage.
  ///
  /// In en, this message translates to:
  /// **'Use it as model \"auto:{id}\" or set it as a key default.'**
  String profileUsage(String id);

  /// No description provided for @minIntelligence.
  ///
  /// In en, this message translates to:
  /// **'Minimum intelligence'**
  String get minIntelligence;

  /// No description provided for @maxPriceOut.
  ///
  /// In en, this message translates to:
  /// **'Max output price (USD / 1M, empty = any)'**
  String get maxPriceOut;

  /// No description provided for @adaptive.
  ///
  /// In en, this message translates to:
  /// **'Adapt to request complexity'**
  String get adaptive;

  /// No description provided for @adaptiveHint.
  ///
  /// In en, this message translates to:
  /// **'Long, code-heavy or tool requests shift weight toward intelligence.'**
  String get adaptiveHint;

  /// No description provided for @exploration.
  ///
  /// In en, this message translates to:
  /// **'Load spreading'**
  String get exploration;

  /// No description provided for @explorationHint.
  ///
  /// In en, this message translates to:
  /// **'Higher values spread requests over more good models.'**
  String get explorationHint;

  /// No description provided for @excludeProviders.
  ///
  /// In en, this message translates to:
  /// **'Exclude providers (comma separated)'**
  String get excludeProviders;

  /// No description provided for @previewTitle.
  ///
  /// In en, this message translates to:
  /// **'Live preview'**
  String get previewTitle;

  /// No description provided for @previewHint.
  ///
  /// In en, this message translates to:
  /// **'What \"auto\" would choose right now, without sending a request.'**
  String get previewHint;

  /// No description provided for @previewPrompt.
  ///
  /// In en, this message translates to:
  /// **'Test prompt'**
  String get previewPrompt;

  /// No description provided for @previewFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Free models only'**
  String get previewFreeOnly;

  /// No description provided for @previewTools.
  ///
  /// In en, this message translates to:
  /// **'Needs tools'**
  String get previewTools;

  /// No description provided for @previewVision.
  ///
  /// In en, this message translates to:
  /// **'Needs vision'**
  String get previewVision;

  /// No description provided for @runPreview.
  ///
  /// In en, this message translates to:
  /// **'Preview'**
  String get runPreview;

  /// No description provided for @score.
  ///
  /// In en, this message translates to:
  /// **'Score'**
  String get score;

  /// No description provided for @excluded.
  ///
  /// In en, this message translates to:
  /// **'Excluded: {reason}'**
  String excluded(String reason);

  /// No description provided for @reasonNotFree.
  ///
  /// In en, this message translates to:
  /// **'not free'**
  String get reasonNotFree;

  /// No description provided for @reasonNoTools.
  ///
  /// In en, this message translates to:
  /// **'no tool support'**
  String get reasonNoTools;

  /// No description provided for @reasonNoVision.
  ///
  /// In en, this message translates to:
  /// **'no vision'**
  String get reasonNoVision;

  /// No description provided for @reasonContext.
  ///
  /// In en, this message translates to:
  /// **'context too small'**
  String get reasonContext;

  /// No description provided for @reasonCooldown.
  ///
  /// In en, this message translates to:
  /// **'cooling down'**
  String get reasonCooldown;

  /// No description provided for @reasonCircuit.
  ///
  /// In en, this message translates to:
  /// **'paused after errors'**
  String get reasonCircuit;

  /// No description provided for @reasonRateLimited.
  ///
  /// In en, this message translates to:
  /// **'rate limited'**
  String get reasonRateLimited;

  /// No description provided for @reasonQuota.
  ///
  /// In en, this message translates to:
  /// **'quota used up'**
  String get reasonQuota;

  /// No description provided for @reasonBudget.
  ///
  /// In en, this message translates to:
  /// **'over budget'**
  String get reasonBudget;

  /// No description provided for @reasonPriceUnknown.
  ///
  /// In en, this message translates to:
  /// **'price unknown'**
  String get reasonPriceUnknown;

  /// No description provided for @reasonDisabled.
  ///
  /// In en, this message translates to:
  /// **'disabled'**
  String get reasonDisabled;

  /// No description provided for @reasonAccount.
  ///
  /// In en, this message translates to:
  /// **'provider unavailable'**
  String get reasonAccount;

  /// No description provided for @reasonAuth.
  ///
  /// In en, this message translates to:
  /// **'key rejected'**
  String get reasonAuth;

  /// No description provided for @reasonNotAvailable.
  ///
  /// In en, this message translates to:
  /// **'not available'**
  String get reasonNotAvailable;

  /// No description provided for @reasonPayment.
  ///
  /// In en, this message translates to:
  /// **'payment required'**
  String get reasonPayment;

  /// No description provided for @reasonNoFreeQuota.
  ///
  /// In en, this message translates to:
  /// **'no free quota on this account'**
  String get reasonNoFreeQuota;

  /// No description provided for @quotasEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Limits'**
  String get quotasEyebrow;

  /// No description provided for @quotasTitle.
  ///
  /// In en, this message translates to:
  /// **'Free quotas'**
  String get quotasTitle;

  /// No description provided for @quotasDescription.
  ///
  /// In en, this message translates to:
  /// **'Usage against each provider’s limits. Limits come from provider headers, documented free tiers or are learned from errors.'**
  String get quotasDescription;

  /// No description provided for @sharedLimits.
  ///
  /// In en, this message translates to:
  /// **'Shared account limits'**
  String get sharedLimits;

  /// No description provided for @perModelLimits.
  ///
  /// In en, this message translates to:
  /// **'Per model'**
  String get perModelLimits;

  /// No description provided for @metricRpm.
  ///
  /// In en, this message translates to:
  /// **'requests / min'**
  String get metricRpm;

  /// No description provided for @metricRpd.
  ///
  /// In en, this message translates to:
  /// **'requests / day'**
  String get metricRpd;

  /// No description provided for @metricTpm.
  ///
  /// In en, this message translates to:
  /// **'tokens / min'**
  String get metricTpm;

  /// No description provided for @metricTpd.
  ///
  /// In en, this message translates to:
  /// **'tokens / day'**
  String get metricTpd;

  /// No description provided for @usedOf.
  ///
  /// In en, this message translates to:
  /// **'{used} of {limit}'**
  String usedOf(String used, String limit);

  /// No description provided for @sourceHeader.
  ///
  /// In en, this message translates to:
  /// **'from provider'**
  String get sourceHeader;

  /// No description provided for @sourceCurated.
  ///
  /// In en, this message translates to:
  /// **'documented'**
  String get sourceCurated;

  /// No description provided for @sourceLearned.
  ///
  /// In en, this message translates to:
  /// **'learned'**
  String get sourceLearned;

  /// No description provided for @sourceManual.
  ///
  /// In en, this message translates to:
  /// **'manual'**
  String get sourceManual;

  /// No description provided for @sourceError.
  ///
  /// In en, this message translates to:
  /// **'observed'**
  String get sourceError;

  /// No description provided for @balance.
  ///
  /// In en, this message translates to:
  /// **'Balance'**
  String get balance;

  /// No description provided for @noQuotaData.
  ///
  /// In en, this message translates to:
  /// **'No limits known yet. They appear after the first requests.'**
  String get noQuotaData;

  /// No description provided for @onlyFreeModels.
  ///
  /// In en, this message translates to:
  /// **'Free models without known limits: {count}'**
  String onlyFreeModels(String count);

  /// No description provided for @logsEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Insights'**
  String get logsEyebrow;

  /// No description provided for @logsTitle.
  ///
  /// In en, this message translates to:
  /// **'Request log'**
  String get logsTitle;

  /// No description provided for @logsDescription.
  ///
  /// In en, this message translates to:
  /// **'Metadata only – prompts and answers are never stored.'**
  String get logsDescription;

  /// No description provided for @filterErrors.
  ///
  /// In en, this message translates to:
  /// **'Errors'**
  String get filterErrors;

  /// No description provided for @filterSuccess.
  ///
  /// In en, this message translates to:
  /// **'Successful'**
  String get filterSuccess;

  /// No description provided for @fallbackTrail.
  ///
  /// In en, this message translates to:
  /// **'Fallback trail'**
  String get fallbackTrail;

  /// No description provided for @attempts.
  ///
  /// In en, this message translates to:
  /// **'{count} attempts'**
  String attempts(String count);

  /// No description provided for @tokensInOut.
  ///
  /// In en, this message translates to:
  /// **'{input} → {output} tokens'**
  String tokensInOut(String input, String output);

  /// No description provided for @ttft.
  ///
  /// In en, this message translates to:
  /// **'TTFT {value}'**
  String ttft(String value);

  /// No description provided for @noLogsTitle.
  ///
  /// In en, this message translates to:
  /// **'No requests yet'**
  String get noLogsTitle;

  /// No description provided for @noLogsMessage.
  ///
  /// In en, this message translates to:
  /// **'Requests appear here as soon as your apps call the endpoint.'**
  String get noLogsMessage;

  /// No description provided for @playgroundEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Test'**
  String get playgroundEyebrow;

  /// No description provided for @playgroundTitle.
  ///
  /// In en, this message translates to:
  /// **'Playground'**
  String get playgroundTitle;

  /// No description provided for @playgroundDescription.
  ///
  /// In en, this message translates to:
  /// **'Chat through the real router with one of your keys.'**
  String get playgroundDescription;

  /// No description provided for @selectKey.
  ///
  /// In en, this message translates to:
  /// **'Router key'**
  String get selectKey;

  /// No description provided for @modelField.
  ///
  /// In en, this message translates to:
  /// **'Model (auto, auto:fast, provider/model)'**
  String get modelField;

  /// No description provided for @systemPrompt.
  ///
  /// In en, this message translates to:
  /// **'System prompt (optional)'**
  String get systemPrompt;

  /// No description provided for @messageHint.
  ///
  /// In en, this message translates to:
  /// **'Write a message…'**
  String get messageHint;

  /// No description provided for @send.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get send;

  /// No description provided for @clearChat.
  ///
  /// In en, this message translates to:
  /// **'Clear'**
  String get clearChat;

  /// No description provided for @routedTo.
  ///
  /// In en, this message translates to:
  /// **'via {model} · {latency}'**
  String routedTo(String model, String latency);

  /// No description provided for @playgroundNeedsKey.
  ///
  /// In en, this message translates to:
  /// **'Create a router key first.'**
  String get playgroundNeedsKey;

  /// No description provided for @settingsTitle.
  ///
  /// In en, this message translates to:
  /// **'Settings'**
  String get settingsTitle;

  /// No description provided for @settingsScopeYou.
  ///
  /// In en, this message translates to:
  /// **'You'**
  String get settingsScopeYou;

  /// No description provided for @settingsScopeServer.
  ///
  /// In en, this message translates to:
  /// **'This server'**
  String get settingsScopeServer;

  /// No description provided for @settingsAccountDescription.
  ///
  /// In en, this message translates to:
  /// **'Your name, email and password.'**
  String get settingsAccountDescription;

  /// No description provided for @settingsAppearanceDescription.
  ///
  /// In en, this message translates to:
  /// **'Theme and language of this dashboard.'**
  String get settingsAppearanceDescription;

  /// No description provided for @settingsConnectDescription.
  ///
  /// In en, this message translates to:
  /// **'Ready-to-paste setup for your SDKs and tools.'**
  String get settingsConnectDescription;

  /// No description provided for @settingsServerDescription.
  ///
  /// In en, this message translates to:
  /// **'Sign-ups, people and model metadata. Only admins see this page.'**
  String get settingsServerDescription;

  /// No description provided for @settingsAppliesInstantly.
  ///
  /// In en, this message translates to:
  /// **'Changes apply instantly'**
  String get settingsAppliesInstantly;

  /// No description provided for @serverTitle.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get serverTitle;

  /// No description provided for @profileTitle.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get profileTitle;

  /// No description provided for @themeLabel.
  ///
  /// In en, this message translates to:
  /// **'Theme'**
  String get themeLabel;

  /// No description provided for @themeHint.
  ///
  /// In en, this message translates to:
  /// **'Follows your device unless you pick one.'**
  String get themeHint;

  /// No description provided for @languageHint.
  ///
  /// In en, this message translates to:
  /// **'Only the dashboard. The gateway is language-neutral.'**
  String get languageHint;

  /// No description provided for @passwordHint.
  ///
  /// In en, this message translates to:
  /// **'At least 12 characters. You will be signed out afterwards.'**
  String get passwordHint;

  /// No description provided for @signOutHint.
  ///
  /// In en, this message translates to:
  /// **'Sign out of NeoRouter in this browser.'**
  String get signOutHint;

  /// No description provided for @appearance.
  ///
  /// In en, this message translates to:
  /// **'Appearance'**
  String get appearance;

  /// No description provided for @themeSystem.
  ///
  /// In en, this message translates to:
  /// **'System'**
  String get themeSystem;

  /// No description provided for @themeLight.
  ///
  /// In en, this message translates to:
  /// **'Light'**
  String get themeLight;

  /// No description provided for @themeDark.
  ///
  /// In en, this message translates to:
  /// **'Dark'**
  String get themeDark;

  /// No description provided for @language.
  ///
  /// In en, this message translates to:
  /// **'Language'**
  String get language;

  /// No description provided for @languageSystem.
  ///
  /// In en, this message translates to:
  /// **'System'**
  String get languageSystem;

  /// No description provided for @account.
  ///
  /// In en, this message translates to:
  /// **'Account'**
  String get account;

  /// No description provided for @displayName.
  ///
  /// In en, this message translates to:
  /// **'Display name'**
  String get displayName;

  /// No description provided for @saveProfileButton.
  ///
  /// In en, this message translates to:
  /// **'Save profile'**
  String get saveProfileButton;

  /// No description provided for @changePassword.
  ///
  /// In en, this message translates to:
  /// **'Change password'**
  String get changePassword;

  /// No description provided for @currentPassword.
  ///
  /// In en, this message translates to:
  /// **'Current password'**
  String get currentPassword;

  /// No description provided for @newPassword.
  ///
  /// In en, this message translates to:
  /// **'New password'**
  String get newPassword;

  /// No description provided for @passwordChanged.
  ///
  /// In en, this message translates to:
  /// **'Password changed. Sign in again.'**
  String get passwordChanged;

  /// No description provided for @connectApps.
  ///
  /// In en, this message translates to:
  /// **'Connect your apps'**
  String get connectApps;

  /// No description provided for @connectAppsHint.
  ///
  /// In en, this message translates to:
  /// **'Replace nrt_… with one of your router keys.'**
  String get connectAppsHint;

  /// No description provided for @registrationOpenLabel.
  ///
  /// In en, this message translates to:
  /// **'Allow new registrations'**
  String get registrationOpenLabel;

  /// No description provided for @registrationOpenHint.
  ///
  /// In en, this message translates to:
  /// **'When off, only existing accounts can sign in.'**
  String get registrationOpenHint;

  /// No description provided for @users.
  ///
  /// In en, this message translates to:
  /// **'Users'**
  String get users;

  /// No description provided for @roleAdmin.
  ///
  /// In en, this message translates to:
  /// **'Admin'**
  String get roleAdmin;

  /// No description provided for @roleUser.
  ///
  /// In en, this message translates to:
  /// **'User'**
  String get roleUser;

  /// No description provided for @you.
  ///
  /// In en, this message translates to:
  /// **'you'**
  String get you;

  /// No description provided for @disableUser.
  ///
  /// In en, this message translates to:
  /// **'Disable'**
  String get disableUser;

  /// No description provided for @enableUser.
  ///
  /// In en, this message translates to:
  /// **'Enable'**
  String get enableUser;

  /// No description provided for @makeAdmin.
  ///
  /// In en, this message translates to:
  /// **'Make admin'**
  String get makeAdmin;

  /// No description provided for @makeUser.
  ///
  /// In en, this message translates to:
  /// **'Make user'**
  String get makeUser;

  /// No description provided for @deleteUserConfirm.
  ///
  /// In en, this message translates to:
  /// **'Delete {name} with all providers, keys and logs?'**
  String deleteUserConfirm(String name);

  /// No description provided for @metadataSources.
  ///
  /// In en, this message translates to:
  /// **'Model metadata'**
  String get metadataSources;

  /// No description provided for @metadataHint.
  ///
  /// In en, this message translates to:
  /// **'Prices, context sizes and intelligence estimates come from models.dev and OpenRouter. An Artificial Analysis key adds benchmark-based intelligence scores.'**
  String get metadataHint;

  /// No description provided for @aaKey.
  ///
  /// In en, this message translates to:
  /// **'Artificial Analysis API key (optional)'**
  String get aaKey;

  /// No description provided for @aaConfigured.
  ///
  /// In en, this message translates to:
  /// **'Artificial Analysis key is configured.'**
  String get aaConfigured;

  /// No description provided for @refreshMetadata.
  ///
  /// In en, this message translates to:
  /// **'Refresh metadata now'**
  String get refreshMetadata;

  /// No description provided for @metadataRefreshed.
  ///
  /// In en, this message translates to:
  /// **'Metadata refreshed.'**
  String get metadataRefreshed;

  /// No description provided for @lastFetched.
  ///
  /// In en, this message translates to:
  /// **'{source}: {time}'**
  String lastFetched(String source, String time);

  /// No description provided for @notFetched.
  ///
  /// In en, this message translates to:
  /// **'not fetched yet'**
  String get notFetched;

  /// No description provided for @navAnalytics.
  ///
  /// In en, this message translates to:
  /// **'Analytics'**
  String get navAnalytics;

  /// No description provided for @analyticsEyebrow.
  ///
  /// In en, this message translates to:
  /// **'Insights'**
  String get analyticsEyebrow;

  /// No description provided for @analyticsTitle.
  ///
  /// In en, this message translates to:
  /// **'Analytics'**
  String get analyticsTitle;

  /// No description provided for @analyticsDescription.
  ///
  /// In en, this message translates to:
  /// **'How your traffic, latency, cost and providers behave over time.'**
  String get analyticsDescription;

  /// No description provided for @chartRequests.
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get chartRequests;

  /// No description provided for @chartSuccess.
  ///
  /// In en, this message translates to:
  /// **'Successful'**
  String get chartSuccess;

  /// No description provided for @chartErrors.
  ///
  /// In en, this message translates to:
  /// **'Errors'**
  String get chartErrors;

  /// No description provided for @chartLatency.
  ///
  /// In en, this message translates to:
  /// **'Latency'**
  String get chartLatency;

  /// No description provided for @chartP50.
  ///
  /// In en, this message translates to:
  /// **'p50'**
  String get chartP50;

  /// No description provided for @chartP95.
  ///
  /// In en, this message translates to:
  /// **'p95'**
  String get chartP95;

  /// No description provided for @chartTokens.
  ///
  /// In en, this message translates to:
  /// **'Tokens'**
  String get chartTokens;

  /// No description provided for @chartInput.
  ///
  /// In en, this message translates to:
  /// **'Input'**
  String get chartInput;

  /// No description provided for @chartOutput.
  ///
  /// In en, this message translates to:
  /// **'Output'**
  String get chartOutput;

  /// No description provided for @chartCost.
  ///
  /// In en, this message translates to:
  /// **'Cost (USD)'**
  String get chartCost;

  /// No description provided for @chartFreeShare.
  ///
  /// In en, this message translates to:
  /// **'Served free'**
  String get chartFreeShare;

  /// No description provided for @providerShare.
  ///
  /// In en, this message translates to:
  /// **'Traffic by provider'**
  String get providerShare;

  /// No description provided for @modelTable.
  ///
  /// In en, this message translates to:
  /// **'Models'**
  String get modelTable;

  /// No description provided for @keyUsage.
  ///
  /// In en, this message translates to:
  /// **'Router keys'**
  String get keyUsage;

  /// No description provided for @apiFormats.
  ///
  /// In en, this message translates to:
  /// **'API formats'**
  String get apiFormats;

  /// No description provided for @attemptOutcomes.
  ///
  /// In en, this message translates to:
  /// **'Attempt outcomes'**
  String get attemptOutcomes;

  /// No description provided for @attemptOutcomesHint.
  ///
  /// In en, this message translates to:
  /// **'Every upstream attempt, including fallbacks and hedges.'**
  String get attemptOutcomesHint;

  /// No description provided for @latencyDistribution.
  ///
  /// In en, this message translates to:
  /// **'Latency distribution'**
  String get latencyDistribution;

  /// No description provided for @colRequests.
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get colRequests;

  /// No description provided for @colSuccess.
  ///
  /// In en, this message translates to:
  /// **'Success'**
  String get colSuccess;

  /// No description provided for @colLatency.
  ///
  /// In en, this message translates to:
  /// **'p50 / p95'**
  String get colLatency;

  /// No description provided for @colTokens.
  ///
  /// In en, this message translates to:
  /// **'Tokens'**
  String get colTokens;

  /// No description provided for @colCost.
  ///
  /// In en, this message translates to:
  /// **'Cost'**
  String get colCost;

  /// No description provided for @upTo.
  ///
  /// In en, this message translates to:
  /// **'≤ {value}'**
  String upTo(String value);

  /// No description provided for @over.
  ///
  /// In en, this message translates to:
  /// **'> {value}'**
  String over(String value);

  /// No description provided for @analyticsTruncated.
  ///
  /// In en, this message translates to:
  /// **'Showing the most recent 100,000 requests of this range.'**
  String get analyticsTruncated;

  /// No description provided for @fetchModels.
  ///
  /// In en, this message translates to:
  /// **'Fetch models'**
  String get fetchModels;

  /// No description provided for @modelsFound.
  ///
  /// In en, this message translates to:
  /// **'{count} models found'**
  String modelsFound(String count);

  /// No description provided for @modelsFetchFailed.
  ///
  /// In en, this message translates to:
  /// **'Could not list models: {message}'**
  String modelsFetchFailed(String message);

  /// No description provided for @manualModels.
  ///
  /// In en, this message translates to:
  /// **'Additional model ids (one per line)'**
  String get manualModels;

  /// No description provided for @manualModelsHint.
  ///
  /// In en, this message translates to:
  /// **'Used when the endpoint has no /models list or misses some models.'**
  String get manualModelsHint;

  /// No description provided for @authHeaderLabel.
  ///
  /// In en, this message translates to:
  /// **'Key header (empty = Authorization: Bearer)'**
  String get authHeaderLabel;

  /// No description provided for @customHeaders.
  ///
  /// In en, this message translates to:
  /// **'Extra headers (Name: value, one per line)'**
  String get customHeaders;

  /// No description provided for @modelsPathLabel.
  ///
  /// In en, this message translates to:
  /// **'Models path (empty = /models)'**
  String get modelsPathLabel;

  /// No description provided for @advancedOptions.
  ///
  /// In en, this message translates to:
  /// **'Advanced'**
  String get advancedOptions;

  /// No description provided for @keyStatus.
  ///
  /// In en, this message translates to:
  /// **'Key: {status}'**
  String keyStatus(String status);

  /// No description provided for @connectOpenRouter.
  ///
  /// In en, this message translates to:
  /// **'Connect with OpenRouter'**
  String get connectOpenRouter;

  /// No description provided for @connectOpenRouterHint.
  ///
  /// In en, this message translates to:
  /// **'One click: sign in at OpenRouter and the key is created and stored automatically.'**
  String get connectOpenRouterHint;

  /// No description provided for @orManualKey.
  ///
  /// In en, this message translates to:
  /// **'or paste a key'**
  String get orManualKey;

  /// No description provided for @openRouterConnected.
  ///
  /// In en, this message translates to:
  /// **'OpenRouter connected – models imported.'**
  String get openRouterConnected;

  /// No description provided for @openRouterFailed.
  ///
  /// In en, this message translates to:
  /// **'OpenRouter connection failed: {message}'**
  String openRouterFailed(String message);

  /// No description provided for @navAdmin.
  ///
  /// In en, this message translates to:
  /// **'Admin'**
  String get navAdmin;

  /// No description provided for @adminDescription.
  ///
  /// In en, this message translates to:
  /// **'Run this server: accounts, sign-ups, server details and model metadata.'**
  String get adminDescription;

  /// No description provided for @adminSearchHint.
  ///
  /// In en, this message translates to:
  /// **'Search admin settings, e.g. registration'**
  String get adminSearchHint;

  /// No description provided for @nothingMatches.
  ///
  /// In en, this message translates to:
  /// **'Nothing matches'**
  String get nothingMatches;

  /// No description provided for @registrationTitle.
  ///
  /// In en, this message translates to:
  /// **'Sign-ups'**
  String get registrationTitle;

  /// No description provided for @usersCount.
  ///
  /// In en, this message translates to:
  /// **'{count} accounts on this server.'**
  String usersCount(String count);

  /// No description provided for @serverInfoHint.
  ///
  /// In en, this message translates to:
  /// **'What this NeoRouter instance runs and where apps reach it.'**
  String get serverInfoHint;

  /// No description provided for @versionLabel.
  ///
  /// In en, this message translates to:
  /// **'Version'**
  String get versionLabel;

  /// No description provided for @capabilitiesLabel.
  ///
  /// In en, this message translates to:
  /// **'Capabilities'**
  String get capabilitiesLabel;

  /// No description provided for @settingsSearchHint.
  ///
  /// In en, this message translates to:
  /// **'Search settings'**
  String get settingsSearchHint;

  /// No description provided for @settingsScopeApp.
  ///
  /// In en, this message translates to:
  /// **'This app'**
  String get settingsScopeApp;

  /// No description provided for @settingsScopeAppHint.
  ///
  /// In en, this message translates to:
  /// **'Applies to this browser'**
  String get settingsScopeAppHint;

  /// No description provided for @settingsScopeYouHint.
  ///
  /// In en, this message translates to:
  /// **'Saved to your account'**
  String get settingsScopeYouHint;

  /// No description provided for @settingsSavedOnSubmit.
  ///
  /// In en, this message translates to:
  /// **'Saved when you press Save'**
  String get settingsSavedOnSubmit;
}

class _AppL10nDelegate extends LocalizationsDelegate<AppL10n> {
  const _AppL10nDelegate();

  @override
  Future<AppL10n> load(Locale locale) {
    return SynchronousFuture<AppL10n>(lookupAppL10n(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['de', 'en'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppL10nDelegate old) => false;
}

AppL10n lookupAppL10n(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'de':
      return AppL10nDe();
    case 'en':
      return AppL10nEn();
  }

  throw FlutterError(
    'AppL10n.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
