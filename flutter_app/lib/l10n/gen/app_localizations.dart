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
  /// **'Connections'**
  String get navProviders;

  /// No description provided for @navModels.
  ///
  /// In en, this message translates to:
  /// **'Models'**
  String get navModels;

  /// No description provided for @navKeys.
  ///
  /// In en, this message translates to:
  /// **'API keys'**
  String get navKeys;

  /// No description provided for @navRouting.
  ///
  /// In en, this message translates to:
  /// **'Smart routing'**
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
  /// **'Nothing here yet'**
  String get noTrafficTitle;

  /// No description provided for @noTrafficMessage.
  ///
  /// In en, this message translates to:
  /// **'Once your apps send requests, you will see how much, how fast and which models answered.'**
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
  /// **'Requests skip it until this is fixed.'**
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
  /// **'No requests yet. Send your first message in the Playground.'**
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
  /// **'Connections'**
  String get providersTitle;

  /// No description provided for @providersDescription.
  ///
  /// In en, this message translates to:
  /// **'The AI providers NeoRouter can use. Keys are checked when you add them and stored encrypted.'**
  String get providersDescription;

  /// No description provided for @addProvider.
  ///
  /// In en, this message translates to:
  /// **'Add connection'**
  String get addProvider;

  /// No description provided for @chooseProvider.
  ///
  /// In en, this message translates to:
  /// **'Add a connection'**
  String get chooseProvider;

  /// No description provided for @searchProviders.
  ///
  /// In en, this message translates to:
  /// **'Search providers…'**
  String get searchProviders;

  /// No description provided for @categoryFree.
  ///
  /// In en, this message translates to:
  /// **'Free to start'**
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

  /// No description provided for @categorySearch.
  ///
  /// In en, this message translates to:
  /// **'Web search'**
  String get categorySearch;

  /// No description provided for @categoryPlan.
  ///
  /// In en, this message translates to:
  /// **'Subscriptions'**
  String get categoryPlan;

  /// No description provided for @kindFree.
  ///
  /// In en, this message translates to:
  /// **'Free models'**
  String get kindFree;

  /// No description provided for @kindFreeHint.
  ///
  /// In en, this message translates to:
  /// **'Never cost anything.'**
  String get kindFreeHint;

  /// No description provided for @kindPlan.
  ///
  /// In en, this message translates to:
  /// **'Prepaid plans'**
  String get kindPlan;

  /// No description provided for @kindPlanHint.
  ///
  /// In en, this message translates to:
  /// **'Use the allowance of plans you already pay for, like ChatGPT.'**
  String get kindPlanHint;

  /// No description provided for @kindPaid.
  ///
  /// In en, this message translates to:
  /// **'Paid models'**
  String get kindPaid;

  /// No description provided for @kindPaidHint.
  ///
  /// In en, this message translates to:
  /// **'Billed per use, within the spending limit below.'**
  String get kindPaidHint;

  /// No description provided for @kindFreeShort.
  ///
  /// In en, this message translates to:
  /// **'Free'**
  String get kindFreeShort;

  /// No description provided for @kindPlanShort.
  ///
  /// In en, this message translates to:
  /// **'Plans'**
  String get kindPlanShort;

  /// No description provided for @kindPaidShort.
  ///
  /// In en, this message translates to:
  /// **'Paid'**
  String get kindPaidShort;

  /// No description provided for @pickOneKind.
  ///
  /// In en, this message translates to:
  /// **'Pick at least one kind of model.'**
  String get pickOneKind;

  /// No description provided for @spendingLimitPer.
  ///
  /// In en, this message translates to:
  /// **'per'**
  String get spendingLimitPer;

  /// No description provided for @budgetFallsBack.
  ///
  /// In en, this message translates to:
  /// **'Empty means no limit. Once it is used up, the key keeps going with free models and plans.'**
  String get budgetFallsBack;

  /// No description provided for @budgetStops.
  ///
  /// In en, this message translates to:
  /// **'Empty means no limit. Once it is used up, the key stops until the reset.'**
  String get budgetStops;

  /// No description provided for @chatgptSignIn.
  ///
  /// In en, this message translates to:
  /// **'Sign in with ChatGPT'**
  String get chatgptSignIn;

  /// No description provided for @chatgptSignInAgain.
  ///
  /// In en, this message translates to:
  /// **'Sign in again'**
  String get chatgptSignInAgain;

  /// No description provided for @chatgptSignInHint.
  ///
  /// In en, this message translates to:
  /// **'NeoRouter shows a code. Enter it on the OpenAI page with the account that has your ChatGPT plan.'**
  String get chatgptSignInHint;

  /// No description provided for @chatgptEnterCode.
  ///
  /// In en, this message translates to:
  /// **'Enter this code at OpenAI'**
  String get chatgptEnterCode;

  /// No description provided for @chatgptOpenPage.
  ///
  /// In en, this message translates to:
  /// **'Open the OpenAI page'**
  String get chatgptOpenPage;

  /// No description provided for @chatgptWaiting.
  ///
  /// In en, this message translates to:
  /// **'Waiting for you to confirm…'**
  String get chatgptWaiting;

  /// No description provided for @chatgptExpired.
  ///
  /// In en, this message translates to:
  /// **'The code expired. Start again.'**
  String get chatgptExpired;

  /// No description provided for @chatgptConnected.
  ///
  /// In en, this message translates to:
  /// **'ChatGPT connected'**
  String get chatgptConnected;

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
  /// **'Choose the free plan only if no payment method is on file with this provider. NeoRouter double-checks where it can.'**
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
  /// **'Connect'**
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
  /// **'Working'**
  String get statusHealthy;

  /// No description provided for @statusInvalid.
  ///
  /// In en, this message translates to:
  /// **'Key rejected'**
  String get statusInvalid;

  /// No description provided for @statusError.
  ///
  /// In en, this message translates to:
  /// **'Not reachable'**
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
  /// **'Remove connection'**
  String get removeProvider;

  /// No description provided for @removeProviderConfirm.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}? Its models are no longer used. You can add it again at any time.'**
  String removeProviderConfirm(String name);

  /// No description provided for @editProvider.
  ///
  /// In en, this message translates to:
  /// **'Edit'**
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
  /// **'No connections yet'**
  String get noProvidersTitle;

  /// No description provided for @noProvidersMessage.
  ///
  /// In en, this message translates to:
  /// **'Connect an AI provider and NeoRouter imports all of its models.'**
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
  /// **'Everything your connections offer. NeoRouter learns how smart, fast and reliable each model is and picks the best one for you.'**
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
  /// **'Problems'**
  String get filterIssues;

  /// No description provided for @intelligence.
  ///
  /// In en, this message translates to:
  /// **'Smart'**
  String get intelligence;

  /// No description provided for @speed.
  ///
  /// In en, this message translates to:
  /// **'Fast'**
  String get speed;

  /// No description provided for @reliability.
  ///
  /// In en, this message translates to:
  /// **'Reliable'**
  String get reliability;

  /// No description provided for @cost.
  ///
  /// In en, this message translates to:
  /// **'Cheap'**
  String get cost;

  /// No description provided for @price.
  ///
  /// In en, this message translates to:
  /// **'Price'**
  String get price;

  /// No description provided for @pricePerMillion.
  ///
  /// In en, this message translates to:
  /// **'{input} / {output} per 1M tokens'**
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

  /// No description provided for @freeReasonBilledObserved.
  ///
  /// In en, this message translates to:
  /// **'Paid: the provider reported a charge for it'**
  String get freeReasonBilledObserved;

  /// No description provided for @freeReasonNoFreeTier.
  ///
  /// In en, this message translates to:
  /// **'Paid: no free quota on this account'**
  String get freeReasonNoFreeTier;

  /// No description provided for @freeReasonNotInFreeTier.
  ///
  /// In en, this message translates to:
  /// **'Paid: not part of the free tier'**
  String get freeReasonNotInFreeTier;

  /// No description provided for @freeReasonBillingActive.
  ///
  /// In en, this message translates to:
  /// **'Paid: billing is active on this account'**
  String get freeReasonBillingActive;

  /// No description provided for @freeReasonBillingUnverified.
  ///
  /// In en, this message translates to:
  /// **'Not free until the free plan is verified'**
  String get freeReasonBillingUnverified;

  /// No description provided for @billingVerifiedFree.
  ///
  /// In en, this message translates to:
  /// **'Free plan verified'**
  String get billingVerifiedFree;

  /// No description provided for @billingDetectedPaid.
  ///
  /// In en, this message translates to:
  /// **'Billing active'**
  String get billingDetectedPaid;

  /// No description provided for @billingUnverified.
  ///
  /// In en, this message translates to:
  /// **'Free plan not verified'**
  String get billingUnverified;

  /// No description provided for @modelSettings.
  ///
  /// In en, this message translates to:
  /// **'Model settings'**
  String get modelSettings;

  /// No description provided for @overrideFree.
  ///
  /// In en, this message translates to:
  /// **'Free or paid'**
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
  /// **'Turn all on'**
  String get enableAll;

  /// No description provided for @disableAll.
  ///
  /// In en, this message translates to:
  /// **'Turn all off'**
  String get disableAll;

  /// No description provided for @enableFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Use only free models'**
  String get enableFreeOnly;

  /// No description provided for @measured.
  ///
  /// In en, this message translates to:
  /// **'{latency} · {tps} tokens/s · {samples} requests'**
  String measured(String latency, String tps, String samples);

  /// No description provided for @notMeasured.
  ///
  /// In en, this message translates to:
  /// **'Not used yet'**
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
  /// **'No models found'**
  String get noModelsTitle;

  /// No description provided for @noModelsMessage.
  ///
  /// In en, this message translates to:
  /// **'Try another filter or connect another provider.'**
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
  /// **'API keys'**
  String get keysTitle;

  /// No description provided for @keysDescription.
  ///
  /// In en, this message translates to:
  /// **'Keys let your apps use NeoRouter. Decide per key whether it may use paid models and how much it may spend.'**
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

  /// No description provided for @budget.
  ///
  /// In en, this message translates to:
  /// **'Limit in USD (empty = no limit)'**
  String get budget;

  /// No description provided for @budgetPeriod.
  ///
  /// In en, this message translates to:
  /// **'Budget period'**
  String get budgetPeriod;

  /// No description provided for @periodTotal.
  ///
  /// In en, this message translates to:
  /// **'one-time'**
  String get periodTotal;

  /// No description provided for @periodMonthly.
  ///
  /// In en, this message translates to:
  /// **'month'**
  String get periodMonthly;

  /// No description provided for @periodDaily.
  ///
  /// In en, this message translates to:
  /// **'day'**
  String get periodDaily;

  /// No description provided for @budgetResets.
  ///
  /// In en, this message translates to:
  /// **'Budget applies'**
  String get budgetResets;

  /// No description provided for @budgetResetsHint.
  ///
  /// In en, this message translates to:
  /// **'A one-time budget does not refill.'**
  String get budgetResetsHint;

  /// No description provided for @paceAhead.
  ///
  /// In en, this message translates to:
  /// **'Plenty left: better models are allowed'**
  String get paceAhead;

  /// No description provided for @paceOnTrack.
  ///
  /// In en, this message translates to:
  /// **'On track for the period'**
  String get paceOnTrack;

  /// No description provided for @paceTight.
  ///
  /// In en, this message translates to:
  /// **'At this pace about {forecast} by the reset: NeoRouter prefers cheaper models'**
  String paceTight(String forecast);

  /// No description provided for @paceOver.
  ///
  /// In en, this message translates to:
  /// **'Used up until the reset'**
  String get paceOver;

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
  /// **'Routing style for \"auto\"'**
  String get defaultProfile;

  /// No description provided for @keySecretTitle.
  ///
  /// In en, this message translates to:
  /// **'Copy your new key'**
  String get keySecretTitle;

  /// No description provided for @keySecretBody.
  ///
  /// In en, this message translates to:
  /// **'For your security it is shown only once. Paste it into your app where it asks for an API key.'**
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
  /// **'{spent} of {budget} spent'**
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
  /// **'No API keys yet'**
  String get noKeysTitle;

  /// No description provided for @noKeysMessage.
  ///
  /// In en, this message translates to:
  /// **'Create a key and paste it into any app that works with OpenAI or Claude.'**
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
  /// **'Smart routing'**
  String get routingTitle;

  /// No description provided for @routingDescription.
  ///
  /// In en, this message translates to:
  /// **'When an app asks for \"auto\", NeoRouter picks the model for each request. Choose what matters most to you.'**
  String get routingDescription;

  /// No description provided for @presetBalanced.
  ///
  /// In en, this message translates to:
  /// **'Balanced'**
  String get presetBalanced;

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

  /// No description provided for @presetCustom.
  ///
  /// In en, this message translates to:
  /// **'My style'**
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
  /// **'Name'**
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

  /// No description provided for @previewTitle.
  ///
  /// In en, this message translates to:
  /// **'Right now it would choose'**
  String get previewTitle;

  /// No description provided for @previewHint.
  ///
  /// In en, this message translates to:
  /// **'Which models \"{style}\" would choose right now – nothing is sent.'**
  String previewHint(String style);

  /// No description provided for @previewPrompt.
  ///
  /// In en, this message translates to:
  /// **'Test prompt'**
  String get previewPrompt;

  /// No description provided for @previewFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Only free models'**
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
  /// **'Check'**
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
  /// **'Requests per minute'**
  String get metricRpm;

  /// No description provided for @metricRpd.
  ///
  /// In en, this message translates to:
  /// **'Requests per day'**
  String get metricRpd;

  /// No description provided for @metricTpm.
  ///
  /// In en, this message translates to:
  /// **'Tokens per minute'**
  String get metricTpm;

  /// No description provided for @metricTpd.
  ///
  /// In en, this message translates to:
  /// **'Tokens per day'**
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
  /// **'No limits reported yet. They appear after the first requests.'**
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
  /// **'API key'**
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
  /// **'Message NeoRouter…'**
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
  /// **'{model} · {latency}'**
  String routedTo(String model, String latency);

  /// No description provided for @playgroundNeedsKey.
  ///
  /// In en, this message translates to:
  /// **'Create an API key first – the Playground uses it just like your apps do.'**
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

  /// No description provided for @securityTitle.
  ///
  /// In en, this message translates to:
  /// **'Security'**
  String get securityTitle;

  /// No description provided for @twoFactorTitle.
  ///
  /// In en, this message translates to:
  /// **'Two-factor authentication'**
  String get twoFactorTitle;

  /// No description provided for @twoFactorOn.
  ///
  /// In en, this message translates to:
  /// **'2FA on'**
  String get twoFactorOn;

  /// No description provided for @twoFactorOff.
  ///
  /// In en, this message translates to:
  /// **'2FA off'**
  String get twoFactorOff;

  /// No description provided for @twoFactorDisabledHint.
  ///
  /// In en, this message translates to:
  /// **'Ask for a code from an authenticator app at every sign-in.'**
  String get twoFactorDisabledHint;

  /// No description provided for @twoFactorEnabledHint.
  ///
  /// In en, this message translates to:
  /// **'Sign-in asks for an authenticator code. {count} recovery codes left.'**
  String twoFactorEnabledHint(String count);

  /// No description provided for @twoFactorEnable.
  ///
  /// In en, this message translates to:
  /// **'Turn on'**
  String get twoFactorEnable;

  /// No description provided for @twoFactorDisable.
  ///
  /// In en, this message translates to:
  /// **'Turn off'**
  String get twoFactorDisable;

  /// No description provided for @twoFactorNewCodes.
  ///
  /// In en, this message translates to:
  /// **'New recovery codes'**
  String get twoFactorNewCodes;

  /// No description provided for @twoFactorPasswordPrompt.
  ///
  /// In en, this message translates to:
  /// **'Confirm your password to set up two-factor authentication.'**
  String get twoFactorPasswordPrompt;

  /// No description provided for @twoFactorContinue.
  ///
  /// In en, this message translates to:
  /// **'Continue'**
  String get twoFactorContinue;

  /// No description provided for @twoFactorScanHint.
  ///
  /// In en, this message translates to:
  /// **'Scan the QR code with an authenticator app (1Password, Google Authenticator, Authy, …) or enter the key by hand. Then type the 6-digit code it shows.'**
  String get twoFactorScanHint;

  /// No description provided for @twoFactorManualKey.
  ///
  /// In en, this message translates to:
  /// **'Setup key'**
  String get twoFactorManualKey;

  /// No description provided for @twoFactorCode.
  ///
  /// In en, this message translates to:
  /// **'Authenticator code'**
  String get twoFactorCode;

  /// No description provided for @twoFactorCodeHint.
  ///
  /// In en, this message translates to:
  /// **'6-digit code or a recovery code'**
  String get twoFactorCodeHint;

  /// No description provided for @twoFactorCodeOrRecovery.
  ///
  /// In en, this message translates to:
  /// **'Authenticator or recovery code'**
  String get twoFactorCodeOrRecovery;

  /// No description provided for @twoFactorVerify.
  ///
  /// In en, this message translates to:
  /// **'Verify'**
  String get twoFactorVerify;

  /// No description provided for @twoFactorCodesHint.
  ///
  /// In en, this message translates to:
  /// **'Store these recovery codes somewhere safe. Each one works once if you lose your authenticator. They are shown only now.'**
  String get twoFactorCodesHint;

  /// No description provided for @twoFactorCodesSaved.
  ///
  /// In en, this message translates to:
  /// **'I saved them'**
  String get twoFactorCodesSaved;

  /// No description provided for @twoFactorDisablePrompt.
  ///
  /// In en, this message translates to:
  /// **'Enter your password and a current code to turn off two-factor authentication.'**
  String get twoFactorDisablePrompt;

  /// No description provided for @twoFactorNewCodesPrompt.
  ///
  /// In en, this message translates to:
  /// **'New codes replace all old ones. Enter your password and a current code.'**
  String get twoFactorNewCodesPrompt;

  /// No description provided for @twoFactorDisabledNotice.
  ///
  /// In en, this message translates to:
  /// **'Two-factor authentication is off.'**
  String get twoFactorDisabledNotice;

  /// No description provided for @twoFactorBack.
  ///
  /// In en, this message translates to:
  /// **'Use a different account'**
  String get twoFactorBack;

  /// No description provided for @twoFactorLoginTitle.
  ///
  /// In en, this message translates to:
  /// **'Two-step sign-in'**
  String get twoFactorLoginTitle;

  /// No description provided for @twoFactorLoginSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Enter the code from your authenticator app.'**
  String get twoFactorLoginSubtitle;

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

  /// No description provided for @navHome.
  ///
  /// In en, this message translates to:
  /// **'Home'**
  String get navHome;

  /// No description provided for @navActivity.
  ///
  /// In en, this message translates to:
  /// **'Activity'**
  String get navActivity;

  /// No description provided for @navSectionServer.
  ///
  /// In en, this message translates to:
  /// **'Server'**
  String get navSectionServer;

  /// No description provided for @capChat.
  ///
  /// In en, this message translates to:
  /// **'Chat'**
  String get capChat;

  /// No description provided for @capEmbedding.
  ///
  /// In en, this message translates to:
  /// **'Embeddings'**
  String get capEmbedding;

  /// No description provided for @capRerank.
  ///
  /// In en, this message translates to:
  /// **'Ranking'**
  String get capRerank;

  /// No description provided for @capImage.
  ///
  /// In en, this message translates to:
  /// **'Images'**
  String get capImage;

  /// No description provided for @capAudio.
  ///
  /// In en, this message translates to:
  /// **'Audio'**
  String get capAudio;

  /// No description provided for @capSpeech.
  ///
  /// In en, this message translates to:
  /// **'Speech'**
  String get capSpeech;

  /// No description provided for @capSearch.
  ///
  /// In en, this message translates to:
  /// **'Web search'**
  String get capSearch;

  /// No description provided for @capTranscription.
  ///
  /// In en, this message translates to:
  /// **'Transcription'**
  String get capTranscription;

  /// No description provided for @capVideo.
  ///
  /// In en, this message translates to:
  /// **'Video'**
  String get capVideo;

  /// No description provided for @capModeration.
  ///
  /// In en, this message translates to:
  /// **'System One models'**
  String get capModeration;

  /// No description provided for @capOther.
  ///
  /// In en, this message translates to:
  /// **'Other'**
  String get capOther;

  /// No description provided for @capChatHint.
  ///
  /// In en, this message translates to:
  /// **'Text and vision models that answer messages.'**
  String get capChatHint;

  /// No description provided for @capEmbeddingHint.
  ///
  /// In en, this message translates to:
  /// **'Turn text into vectors for search and memory.'**
  String get capEmbeddingHint;

  /// No description provided for @capRerankHint.
  ///
  /// In en, this message translates to:
  /// **'Sort search results by relevance.'**
  String get capRerankHint;

  /// No description provided for @capImageHint.
  ///
  /// In en, this message translates to:
  /// **'Create and edit images.'**
  String get capImageHint;

  /// No description provided for @capAudioHint.
  ///
  /// In en, this message translates to:
  /// **'Speech to text, text to speech and music.'**
  String get capAudioHint;

  /// No description provided for @capSpeechHint.
  ///
  /// In en, this message translates to:
  /// **'Read text aloud in a natural voice.'**
  String get capSpeechHint;

  /// No description provided for @capSearchHint.
  ///
  /// In en, this message translates to:
  /// **'Find current pages on the web, for you and as a tool for chat models.'**
  String get capSearchHint;

  /// No description provided for @capTranscriptionHint.
  ///
  /// In en, this message translates to:
  /// **'Turn recordings into text and subtitles.'**
  String get capTranscriptionHint;

  /// No description provided for @capVideoHint.
  ///
  /// In en, this message translates to:
  /// **'Create short videos from a description.'**
  String get capVideoHint;

  /// No description provided for @capLive.
  ///
  /// In en, this message translates to:
  /// **'Live voice'**
  String get capLive;

  /// No description provided for @capLiveHint.
  ///
  /// In en, this message translates to:
  /// **'Talk in real time: the model listens and answers with its own voice.'**
  String get capLiveHint;

  /// No description provided for @capModerationHint.
  ///
  /// In en, this message translates to:
  /// **'Fast decision models such as Jev: they classify, check and decide in a split second instead of writing long answers.'**
  String get capModerationHint;

  /// No description provided for @capOtherHint.
  ///
  /// In en, this message translates to:
  /// **'Specialised models such as translation or document parsing.'**
  String get capOtherHint;

  /// No description provided for @capComingSoon.
  ///
  /// In en, this message translates to:
  /// **'NeoRouter already found {count} of these models in your connections and will route them in a coming update.'**
  String capComingSoon(String count);

  /// No description provided for @capSoon.
  ///
  /// In en, this message translates to:
  /// **'Soon'**
  String get capSoon;

  /// No description provided for @greetMorning.
  ///
  /// In en, this message translates to:
  /// **'Good morning, {name}'**
  String greetMorning(String name);

  /// No description provided for @greetAfternoon.
  ///
  /// In en, this message translates to:
  /// **'Good afternoon, {name}'**
  String greetAfternoon(String name);

  /// No description provided for @greetEvening.
  ///
  /// In en, this message translates to:
  /// **'Good evening, {name}'**
  String greetEvening(String name);

  /// No description provided for @homeStatusOk.
  ///
  /// In en, this message translates to:
  /// **'Everything is running. {count} connections are working.'**
  String homeStatusOk(String count);

  /// No description provided for @homeStatusIssues.
  ///
  /// In en, this message translates to:
  /// **'{count} connection needs your attention. Requests use the others meanwhile.'**
  String homeStatusIssues(String count);

  /// No description provided for @homeStatusEmpty.
  ///
  /// In en, this message translates to:
  /// **'Connect an AI provider to get started.'**
  String get homeStatusEmpty;

  /// No description provided for @homeLast7Days.
  ///
  /// In en, this message translates to:
  /// **'Last 7 days'**
  String get homeLast7Days;

  /// No description provided for @metricRequests.
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get metricRequests;

  /// No description provided for @metricAnswered.
  ///
  /// In en, this message translates to:
  /// **'Answered'**
  String get metricAnswered;

  /// No description provided for @metricSpent.
  ///
  /// In en, this message translates to:
  /// **'Spent'**
  String get metricSpent;

  /// No description provided for @metricFreeShare.
  ///
  /// In en, this message translates to:
  /// **'{percent} % free'**
  String metricFreeShare(String percent);

  /// No description provided for @metricResponse.
  ///
  /// In en, this message translates to:
  /// **'Avg. response time'**
  String get metricResponse;

  /// No description provided for @homeSeeAll.
  ///
  /// In en, this message translates to:
  /// **'See all'**
  String get homeSeeAll;

  /// No description provided for @homeRecentActivity.
  ///
  /// In en, this message translates to:
  /// **'Recent activity'**
  String get homeRecentActivity;

  /// No description provided for @homeModelsLine.
  ///
  /// In en, this message translates to:
  /// **'{total} models · {free} free'**
  String homeModelsLine(String total, String free);

  /// No description provided for @homeUseTitle.
  ///
  /// In en, this message translates to:
  /// **'Use NeoRouter in your apps'**
  String get homeUseTitle;

  /// No description provided for @homeUseBody.
  ///
  /// In en, this message translates to:
  /// **'Paste this address and one of your API keys into any app that works with OpenAI or Claude.'**
  String get homeUseBody;

  /// No description provided for @homeGetKey.
  ///
  /// In en, this message translates to:
  /// **'Get an API key'**
  String get homeGetKey;

  /// No description provided for @homeShowExamples.
  ///
  /// In en, this message translates to:
  /// **'Show code examples'**
  String get homeShowExamples;

  /// No description provided for @homeHideExamples.
  ///
  /// In en, this message translates to:
  /// **'Hide code examples'**
  String get homeHideExamples;

  /// No description provided for @setupTitle.
  ///
  /// In en, this message translates to:
  /// **'Finish setting up'**
  String get setupTitle;

  /// No description provided for @setupProgress.
  ///
  /// In en, this message translates to:
  /// **'{done} of {total} done'**
  String setupProgress(String done, String total);

  /// No description provided for @setupStep1Title.
  ///
  /// In en, this message translates to:
  /// **'Connect an AI provider'**
  String get setupStep1Title;

  /// No description provided for @setupStep1Body.
  ///
  /// In en, this message translates to:
  /// **'Add a key from Groq, Google, OpenRouter or another provider. NeoRouter finds all its models.'**
  String get setupStep1Body;

  /// No description provided for @setupStep2Title.
  ///
  /// In en, this message translates to:
  /// **'Create an API key'**
  String get setupStep2Title;

  /// No description provided for @setupStep2Body.
  ///
  /// In en, this message translates to:
  /// **'One key for your apps, optionally limited to free models or a budget.'**
  String get setupStep2Body;

  /// No description provided for @setupStep3Title.
  ///
  /// In en, this message translates to:
  /// **'Send your first message'**
  String get setupStep3Title;

  /// No description provided for @setupStep3Body.
  ///
  /// In en, this message translates to:
  /// **'Try it in the Playground.'**
  String get setupStep3Body;

  /// No description provided for @setupStart.
  ///
  /// In en, this message translates to:
  /// **'Start'**
  String get setupStart;

  /// No description provided for @problemTitle.
  ///
  /// In en, this message translates to:
  /// **'{name} needs attention'**
  String problemTitle(String name);

  /// No description provided for @problemKeyRejected.
  ///
  /// In en, this message translates to:
  /// **'The provider rejected the key. Update it to use this connection again.'**
  String get problemKeyRejected;

  /// No description provided for @requestFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed: {reason}'**
  String requestFailed(String reason);

  /// No description provided for @agoNow.
  ///
  /// In en, this message translates to:
  /// **'just now'**
  String get agoNow;

  /// No description provided for @agoMinutes.
  ///
  /// In en, this message translates to:
  /// **'{count} min ago'**
  String agoMinutes(String count);

  /// No description provided for @agoHours.
  ///
  /// In en, this message translates to:
  /// **'{count} h ago'**
  String agoHours(String count);

  /// No description provided for @activityDescription.
  ///
  /// In en, this message translates to:
  /// **'What your apps asked for and how NeoRouter answered. Only metadata is kept – never your messages.'**
  String get activityDescription;

  /// No description provided for @tabOverview.
  ///
  /// In en, this message translates to:
  /// **'Overview'**
  String get tabOverview;

  /// No description provided for @tabRequests.
  ///
  /// In en, this message translates to:
  /// **'Requests'**
  String get tabRequests;

  /// No description provided for @metricTokens.
  ///
  /// In en, this message translates to:
  /// **'{count} tokens'**
  String metricTokens(String count);

  /// No description provided for @metricTypicalResponse.
  ///
  /// In en, this message translates to:
  /// **'Typical response'**
  String get metricTypicalResponse;

  /// No description provided for @metricSlowest.
  ///
  /// In en, this message translates to:
  /// **'slowest 5 %: {value}'**
  String metricSlowest(String value);

  /// No description provided for @chartResponseTime.
  ///
  /// In en, this message translates to:
  /// **'Response time'**
  String get chartResponseTime;

  /// No description provided for @chartTypical.
  ///
  /// In en, this message translates to:
  /// **'Typical'**
  String get chartTypical;

  /// No description provided for @chartSlowest.
  ///
  /// In en, this message translates to:
  /// **'Slowest 5 %'**
  String get chartSlowest;

  /// No description provided for @topModelsTitle.
  ///
  /// In en, this message translates to:
  /// **'Most used models'**
  String get topModelsTitle;

  /// No description provided for @byConnection.
  ///
  /// In en, this message translates to:
  /// **'By connection'**
  String get byConnection;

  /// No description provided for @byKey.
  ///
  /// In en, this message translates to:
  /// **'By API key'**
  String get byKey;

  /// No description provided for @rankingLine.
  ///
  /// In en, this message translates to:
  /// **'{count} · {latency}'**
  String rankingLine(String count, String latency);

  /// No description provided for @tooltipRequests.
  ///
  /// In en, this message translates to:
  /// **'{count} requests · {errors} failed'**
  String tooltipRequests(String count, String errors);

  /// No description provided for @filterAnswered.
  ///
  /// In en, this message translates to:
  /// **'Answered'**
  String get filterAnswered;

  /// No description provided for @filterFailed.
  ///
  /// In en, this message translates to:
  /// **'Failed'**
  String get filterFailed;

  /// No description provided for @logsPrivacyHint.
  ///
  /// In en, this message translates to:
  /// **'NeoRouter never stores what you or the model wrote – only which model answered, how fast and what it cost.'**
  String get logsPrivacyHint;

  /// No description provided for @detailRequested.
  ///
  /// In en, this message translates to:
  /// **'Asked for'**
  String get detailRequested;

  /// No description provided for @detailKey.
  ///
  /// In en, this message translates to:
  /// **'API key'**
  String get detailKey;

  /// No description provided for @detailResponse.
  ///
  /// In en, this message translates to:
  /// **'Response time'**
  String get detailResponse;

  /// No description provided for @detailFirstToken.
  ///
  /// In en, this message translates to:
  /// **'First words after'**
  String get detailFirstToken;

  /// No description provided for @detailTokens.
  ///
  /// In en, this message translates to:
  /// **'Tokens'**
  String get detailTokens;

  /// No description provided for @detailCost.
  ///
  /// In en, this message translates to:
  /// **'Cost'**
  String get detailCost;

  /// No description provided for @detailFormat.
  ///
  /// In en, this message translates to:
  /// **'Interface'**
  String get detailFormat;

  /// No description provided for @detailTime.
  ///
  /// In en, this message translates to:
  /// **'Time'**
  String get detailTime;

  /// No description provided for @detailRoute.
  ///
  /// In en, this message translates to:
  /// **'Route'**
  String get detailRoute;

  /// No description provided for @outcomeAnswered.
  ///
  /// In en, this message translates to:
  /// **'answered'**
  String get outcomeAnswered;

  /// No description provided for @outcomeSlower.
  ///
  /// In en, this message translates to:
  /// **'was slower – cancelled'**
  String get outcomeSlower;

  /// No description provided for @outcomeBusy.
  ///
  /// In en, this message translates to:
  /// **'was busy – skipped'**
  String get outcomeBusy;

  /// No description provided for @outcomeFailed.
  ///
  /// In en, this message translates to:
  /// **'failed ({reason})'**
  String outcomeFailed(String reason);

  /// No description provided for @tabConnected.
  ///
  /// In en, this message translates to:
  /// **'Connected'**
  String get tabConnected;

  /// No description provided for @tabLimits.
  ///
  /// In en, this message translates to:
  /// **'Usage limits'**
  String get tabLimits;

  /// No description provided for @accountFreePlan.
  ///
  /// In en, this message translates to:
  /// **'Free plan'**
  String get accountFreePlan;

  /// No description provided for @accountPaidPlan.
  ///
  /// In en, this message translates to:
  /// **'Paid account'**
  String get accountPaidPlan;

  /// No description provided for @accountPaidWithFree.
  ///
  /// In en, this message translates to:
  /// **'Paid account · {count} free models'**
  String accountPaidWithFree(String count);

  /// No description provided for @accountFreeModelsOnly.
  ///
  /// In en, this message translates to:
  /// **'Free models only'**
  String get accountFreeModelsOnly;

  /// No description provided for @accountFreeVerified.
  ///
  /// In en, this message translates to:
  /// **'Free plan, verified'**
  String get accountFreeVerified;

  /// No description provided for @accountBillingActive.
  ///
  /// In en, this message translates to:
  /// **'Billing is active – nothing counts as free'**
  String get accountBillingActive;

  /// No description provided for @accountNotVerified.
  ///
  /// In en, this message translates to:
  /// **'Free plan, not verified yet'**
  String get accountNotVerified;

  /// No description provided for @moreActions.
  ///
  /// In en, this message translates to:
  /// **'More actions'**
  String get moreActions;

  /// No description provided for @chooseProviderHint.
  ///
  /// In en, this message translates to:
  /// **'Pick the service you have a key for. Providers marked \"Free models\" can be used without paying.'**
  String get chooseProviderHint;

  /// No description provided for @providerFreeTag.
  ///
  /// In en, this message translates to:
  /// **'Free models'**
  String get providerFreeTag;

  /// No description provided for @getKeyAt.
  ///
  /// In en, this message translates to:
  /// **'Get a key at {name}'**
  String getKeyAt(String name);

  /// No description provided for @billingOptionFree.
  ///
  /// In en, this message translates to:
  /// **'Free tier'**
  String get billingOptionFree;

  /// No description provided for @billingOptionFreeModels.
  ///
  /// In en, this message translates to:
  /// **'Free models only'**
  String get billingOptionFreeModels;

  /// No description provided for @billingOptionFreeModelsHint.
  ///
  /// In en, this message translates to:
  /// **'No credits on this account. NeoRouter uses only its free models and skips the rest.'**
  String get billingOptionFreeModelsHint;

  /// No description provided for @billingOptionFreeHint.
  ///
  /// In en, this message translates to:
  /// **'No payment method on file. NeoRouter only uses the free models.'**
  String get billingOptionFreeHint;

  /// No description provided for @billingOptionPlan.
  ///
  /// In en, this message translates to:
  /// **'Prepaid plan'**
  String get billingOptionPlan;

  /// No description provided for @billingOptionPlanHint.
  ///
  /// In en, this message translates to:
  /// **'A subscription with an included allowance. Used first while there is room; stops when it is used up.'**
  String get billingOptionPlanHint;

  /// No description provided for @billingOptionPlanPaid.
  ///
  /// In en, this message translates to:
  /// **'Plan, then pay per use'**
  String get billingOptionPlanPaid;

  /// No description provided for @billingOptionPlanPaidHint.
  ///
  /// In en, this message translates to:
  /// **'The allowance first; after that requests are billed and count against key budgets.'**
  String get billingOptionPlanPaidHint;

  /// No description provided for @billingOptionPaid.
  ///
  /// In en, this message translates to:
  /// **'Pay per use'**
  String get billingOptionPaid;

  /// No description provided for @billingOptionPaidHint.
  ///
  /// In en, this message translates to:
  /// **'Every request to a paid model is billed and counts against your API keys\' budgets.'**
  String get billingOptionPaidHint;

  /// No description provided for @accountPrepaidPlan.
  ///
  /// In en, this message translates to:
  /// **'Prepaid plan'**
  String get accountPrepaidPlan;

  /// No description provided for @accountPlanThenPaid.
  ///
  /// In en, this message translates to:
  /// **'Plan, then pay per use'**
  String get accountPlanThenPaid;

  /// No description provided for @includedInPlan.
  ///
  /// In en, this message translates to:
  /// **'Included in plan'**
  String get includedInPlan;

  /// No description provided for @metricPlanRequests.
  ///
  /// In en, this message translates to:
  /// **'Plan requests'**
  String get metricPlanRequests;

  /// No description provided for @metricPlan5h.
  ///
  /// In en, this message translates to:
  /// **'Plan, 5-hour window'**
  String get metricPlan5h;

  /// No description provided for @metricPlanWeek.
  ///
  /// In en, this message translates to:
  /// **'Plan, this week'**
  String get metricPlanWeek;

  /// No description provided for @metricPlanMonth.
  ///
  /// In en, this message translates to:
  /// **'Plan, this month'**
  String get metricPlanMonth;

  /// No description provided for @billingPaidHint.
  ///
  /// In en, this message translates to:
  /// **'Requests to paid models are billed by the provider and count against your API keys\' budgets.'**
  String get billingPaidHint;

  /// No description provided for @testConnection.
  ///
  /// In en, this message translates to:
  /// **'Test connection'**
  String get testConnection;

  /// No description provided for @probeOk.
  ///
  /// In en, this message translates to:
  /// **'The key works – {count} models found.'**
  String probeOk(String count);

  /// No description provided for @limitsDescription.
  ///
  /// In en, this message translates to:
  /// **'What is left of each provider\'s allowance today. Per-minute limits reset on their own and NeoRouter routes around them, so only daily allowances and models running low are shown.'**
  String get limitsDescription;

  /// No description provided for @limitResets.
  ///
  /// In en, this message translates to:
  /// **'resets {time}'**
  String limitResets(String time);

  /// No description provided for @limitConcurrent.
  ///
  /// In en, this message translates to:
  /// **'Requests at once'**
  String get limitConcurrent;

  /// No description provided for @limitConcurrentValue.
  ///
  /// In en, this message translates to:
  /// **'up to {count}, learned from its answers'**
  String limitConcurrentValue(String count);

  /// No description provided for @limitConcurrentDocumented.
  ///
  /// In en, this message translates to:
  /// **'up to {count}'**
  String limitConcurrentDocumented(String count);

  /// No description provided for @addAnotherAccount.
  ///
  /// In en, this message translates to:
  /// **'Add another account'**
  String get addAnotherAccount;

  /// No description provided for @limitsModelsRoom.
  ///
  /// In en, this message translates to:
  /// **'{room} of {total} models have room today'**
  String limitsModelsRoom(String room, String total);

  /// No description provided for @filterProvider.
  ///
  /// In en, this message translates to:
  /// **'Filter by connection'**
  String get filterProvider;

  /// No description provided for @allConnections.
  ///
  /// In en, this message translates to:
  /// **'All connections'**
  String get allConnections;

  /// No description provided for @showMore.
  ///
  /// In en, this message translates to:
  /// **'Show more ({count} left)'**
  String showMore(String count);

  /// No description provided for @factWhyFree.
  ///
  /// In en, this message translates to:
  /// **'Free or paid'**
  String get factWhyFree;

  /// No description provided for @factContext.
  ///
  /// In en, this message translates to:
  /// **'Context'**
  String get factContext;

  /// No description provided for @factMeasured.
  ///
  /// In en, this message translates to:
  /// **'Measured'**
  String get factMeasured;

  /// No description provided for @keyAccessTitle.
  ///
  /// In en, this message translates to:
  /// **'What can this key use?'**
  String get keyAccessTitle;

  /// No description provided for @keyBudgetTitle.
  ///
  /// In en, this message translates to:
  /// **'Spending limit'**
  String get keyBudgetTitle;

  /// No description provided for @keyNameHint.
  ///
  /// In en, this message translates to:
  /// **'e.g. Laptop or Home Assistant'**
  String get keyNameHint;

  /// No description provided for @keySecretDone.
  ///
  /// In en, this message translates to:
  /// **'Done'**
  String get keySecretDone;

  /// No description provided for @keyUsed.
  ///
  /// In en, this message translates to:
  /// **'used {time}'**
  String keyUsed(String time);

  /// No description provided for @keyNeverUsed.
  ///
  /// In en, this message translates to:
  /// **'not used yet'**
  String get keyNeverUsed;

  /// No description provided for @revokedKeys.
  ///
  /// In en, this message translates to:
  /// **'Revoked keys ({count})'**
  String revokedKeys(String count);

  /// No description provided for @keysFooter.
  ///
  /// In en, this message translates to:
  /// **'Each app should get its own key. If a key leaks, revoke it – your other apps keep working.'**
  String get keysFooter;

  /// No description provided for @presetBalancedHint.
  ///
  /// In en, this message translates to:
  /// **'A good mix of quality, speed, reliability and cost. Used when nothing else is set.'**
  String get presetBalancedHint;

  /// No description provided for @presetFastHint.
  ///
  /// In en, this message translates to:
  /// **'The shortest wait for an answer, from models that rarely fail.'**
  String get presetFastHint;

  /// No description provided for @presetSmartHint.
  ///
  /// In en, this message translates to:
  /// **'The most capable models, even if they take longer.'**
  String get presetSmartHint;

  /// No description provided for @presetCheaperNote.
  ///
  /// In en, this message translates to:
  /// **'Whatever the profile, of two models with similar quality the cheaper one is used.'**
  String get presetCheaperNote;

  /// No description provided for @autoKeyDefault.
  ///
  /// In en, this message translates to:
  /// **'Auto (key default)'**
  String get autoKeyDefault;

  /// No description provided for @presetBalancedDefault.
  ///
  /// In en, this message translates to:
  /// **'Balanced (default)'**
  String get presetBalancedDefault;

  /// No description provided for @stylesTitle.
  ///
  /// In en, this message translates to:
  /// **'Routing styles'**
  String get stylesTitle;

  /// No description provided for @stylesHint.
  ///
  /// In en, this message translates to:
  /// **'Use a style by asking for its model name, e.g. \"auto:fast\", or choose one per API key.'**
  String get stylesHint;

  /// No description provided for @customStylesTitle.
  ///
  /// In en, this message translates to:
  /// **'Your own styles'**
  String get customStylesTitle;

  /// No description provided for @customStylesHint.
  ///
  /// In en, this message translates to:
  /// **'Decide yourself how much quality, speed, reliability and cost matter.'**
  String get customStylesHint;

  /// No description provided for @createStyle.
  ///
  /// In en, this message translates to:
  /// **'New style'**
  String get createStyle;

  /// No description provided for @editStyle.
  ///
  /// In en, this message translates to:
  /// **'Edit style'**
  String get editStyle;

  /// No description provided for @weightsTitle.
  ///
  /// In en, this message translates to:
  /// **'What matters'**
  String get weightsTitle;

  /// No description provided for @previewDefaultPrompt.
  ///
  /// In en, this message translates to:
  /// **'Explain quicksort in two sentences.'**
  String get previewDefaultPrompt;

  /// No description provided for @previewNone.
  ///
  /// In en, this message translates to:
  /// **'No model fits right now. Connect a provider or turn off \"free models only\".'**
  String get previewNone;

  /// No description provided for @previewSkipped.
  ///
  /// In en, this message translates to:
  /// **'{count} models skipped'**
  String previewSkipped(String count);

  /// No description provided for @previewMostLikely.
  ///
  /// In en, this message translates to:
  /// **'most likely'**
  String get previewMostLikely;

  /// No description provided for @playgroundEmptyTitle.
  ///
  /// In en, this message translates to:
  /// **'What would you like to try?'**
  String get playgroundEmptyTitle;

  /// No description provided for @playgroundEmptyHint.
  ///
  /// In en, this message translates to:
  /// **'Messages go through the real router with your key, so you see which model answers and how fast.'**
  String get playgroundEmptyHint;

  /// No description provided for @suggestion1.
  ///
  /// In en, this message translates to:
  /// **'Explain how a rainbow forms in two sentences'**
  String get suggestion1;

  /// No description provided for @suggestion2.
  ///
  /// In en, this message translates to:
  /// **'Write a friendly reminder for a team meeting'**
  String get suggestion2;

  /// No description provided for @suggestion3.
  ///
  /// In en, this message translates to:
  /// **'Give me three dinner ideas with chickpeas'**
  String get suggestion3;

  /// No description provided for @newChat.
  ///
  /// In en, this message translates to:
  /// **'New chat'**
  String get newChat;

  /// No description provided for @modelPicker.
  ///
  /// In en, this message translates to:
  /// **'Model'**
  String get modelPicker;

  /// No description provided for @modelSpecific.
  ///
  /// In en, this message translates to:
  /// **'Specific model…'**
  String get modelSpecific;

  /// No description provided for @modelSpecificHint.
  ///
  /// In en, this message translates to:
  /// **'e.g. groq/openai/gpt-oss-120b'**
  String get modelSpecificHint;

  /// No description provided for @systemPromptTitle.
  ///
  /// In en, this message translates to:
  /// **'Instructions'**
  String get systemPromptTitle;

  /// No description provided for @systemPromptHint.
  ///
  /// In en, this message translates to:
  /// **'Optional: how the assistant should behave, e.g. \"Answer briefly and in German.\"'**
  String get systemPromptHint;

  /// No description provided for @thinking.
  ///
  /// In en, this message translates to:
  /// **'Thinking…'**
  String get thinking;

  /// No description provided for @enterToSend.
  ///
  /// In en, this message translates to:
  /// **'Enter to send · Shift+Enter for a new line'**
  String get enterToSend;

  /// No description provided for @groupModels.
  ///
  /// In en, this message translates to:
  /// **'Models'**
  String get groupModels;

  /// No description provided for @groupAccess.
  ///
  /// In en, this message translates to:
  /// **'Access'**
  String get groupAccess;

  /// No description provided for @capComingSoonTitle.
  ///
  /// In en, this message translates to:
  /// **'{kind} – coming soon'**
  String capComingSoonTitle(String kind);

  /// No description provided for @capRouter.
  ///
  /// In en, this message translates to:
  /// **'Other routers'**
  String get capRouter;

  /// No description provided for @capRouterHint.
  ///
  /// In en, this message translates to:
  /// **'Services such as OpenRouter Auto or Jev Router that pick a model themselves. NeoRouter does the routing for you, so they are only listed.'**
  String get capRouterHint;

  /// No description provided for @metricSaved.
  ///
  /// In en, this message translates to:
  /// **'Saved with free models'**
  String get metricSaved;

  /// No description provided for @metricSavedCaption.
  ///
  /// In en, this message translates to:
  /// **'{spent} spent · {percent} % free'**
  String metricSavedCaption(String spent, String percent);

  /// No description provided for @styleBuiltIn.
  ///
  /// In en, this message translates to:
  /// **'Built-in styles'**
  String get styleBuiltIn;

  /// No description provided for @styleUseTitle.
  ///
  /// In en, this message translates to:
  /// **'Use this style'**
  String get styleUseTitle;

  /// No description provided for @styleUseHint.
  ///
  /// In en, this message translates to:
  /// **'Ask for this model name in any app, or choose the style for an API key.'**
  String get styleUseHint;

  /// No description provided for @styleUseRow.
  ///
  /// In en, this message translates to:
  /// **'Model name for your apps'**
  String get styleUseRow;

  /// No description provided for @styleWeightsHint.
  ///
  /// In en, this message translates to:
  /// **'How much each quality counts when NeoRouter compares models. A model also has to be decent on everything that matters.'**
  String get styleWeightsHint;

  /// No description provided for @freeMode.
  ///
  /// In en, this message translates to:
  /// **'Free only'**
  String get freeMode;

  /// No description provided for @freeModeOnHint.
  ///
  /// In en, this message translates to:
  /// **'Only free models answer. Turn off to let NeoRouter also use paid models this key may use.'**
  String get freeModeOnHint;

  /// No description provided for @freeModeOffHint.
  ///
  /// In en, this message translates to:
  /// **'Paid models may answer too, within this key\'s budget.'**
  String get freeModeOffHint;

  /// No description provided for @freeModeLocked.
  ///
  /// In en, this message translates to:
  /// **'This API key only uses free models.'**
  String get freeModeLocked;

  /// No description provided for @freeModeLockedExplain.
  ///
  /// In en, this message translates to:
  /// **'This API key is set to \"Only free models\", so it always stays free. To allow paid models, pick another key or change this one under API keys.'**
  String get freeModeLockedExplain;

  /// No description provided for @navProcessing.
  ///
  /// In en, this message translates to:
  /// **'Privacy & compression'**
  String get navProcessing;

  /// No description provided for @processingTitle.
  ///
  /// In en, this message translates to:
  /// **'Privacy & compression'**
  String get processingTitle;

  /// No description provided for @processingDescription.
  ///
  /// In en, this message translates to:
  /// **'What happens to every request before it leaves your server: hide private data from providers and make prompts smaller.'**
  String get processingDescription;

  /// No description provided for @privacyShield.
  ///
  /// In en, this message translates to:
  /// **'Privacy shield'**
  String get privacyShield;

  /// No description provided for @compressionTitle.
  ///
  /// In en, this message translates to:
  /// **'Compression'**
  String get compressionTitle;

  /// No description provided for @privacyHiddenValues.
  ///
  /// In en, this message translates to:
  /// **'Values hidden'**
  String get privacyHiddenValues;

  /// No description provided for @privacyProtectedRequests.
  ///
  /// In en, this message translates to:
  /// **'Protected requests'**
  String get privacyProtectedRequests;

  /// No description provided for @last30Days.
  ///
  /// In en, this message translates to:
  /// **'last 30 days'**
  String get last30Days;

  /// No description provided for @ofRequests.
  ///
  /// In en, this message translates to:
  /// **'of {count} requests'**
  String ofRequests(String count);

  /// No description provided for @privacyHowItWorks.
  ///
  /// In en, this message translates to:
  /// **'Private values are swapped for placeholders like [EMAIL_1] before a request leaves your server. The AI only ever sees the placeholder, and NeoRouter puts the real value back into its answer.'**
  String get privacyHowItWorks;

  /// No description provided for @privacyEnable.
  ///
  /// In en, this message translates to:
  /// **'Hide private data'**
  String get privacyEnable;

  /// No description provided for @privacyEnableHint.
  ///
  /// In en, this message translates to:
  /// **'Applies to every API key and the Playground. Embeddings, image and video prompts and web searches only get the values set to \"Hide only\".'**
  String get privacyEnableHint;

  /// No description provided for @privacyWhatToHide.
  ///
  /// In en, this message translates to:
  /// **'What to hide'**
  String get privacyWhatToHide;

  /// No description provided for @privacyModesHint.
  ///
  /// In en, this message translates to:
  /// **'\"Hide & restore\" puts the value back into the answer. \"Hide only\" keeps it hidden for good – best for passwords and card numbers.'**
  String get privacyModesHint;

  /// No description provided for @modeOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get modeOff;

  /// No description provided for @modeRestore.
  ///
  /// In en, this message translates to:
  /// **'Hide & restore'**
  String get modeRestore;

  /// No description provided for @modeHide.
  ///
  /// In en, this message translates to:
  /// **'Hide only'**
  String get modeHide;

  /// No description provided for @catEmail.
  ///
  /// In en, this message translates to:
  /// **'Email addresses'**
  String get catEmail;

  /// No description provided for @catEmailHint.
  ///
  /// In en, this message translates to:
  /// **'anna@example.com'**
  String get catEmailHint;

  /// No description provided for @catPhone.
  ///
  /// In en, this message translates to:
  /// **'Phone numbers'**
  String get catPhone;

  /// No description provided for @catPhoneHint.
  ///
  /// In en, this message translates to:
  /// **'International and local formats.'**
  String get catPhoneHint;

  /// No description provided for @catIban.
  ///
  /// In en, this message translates to:
  /// **'Bank accounts'**
  String get catIban;

  /// No description provided for @catIbanHint.
  ///
  /// In en, this message translates to:
  /// **'IBAN (checked with its checksum), BIC, and account numbers named as such.'**
  String get catIbanHint;

  /// No description provided for @catCard.
  ///
  /// In en, this message translates to:
  /// **'Payment cards'**
  String get catCard;

  /// No description provided for @catCardHint.
  ///
  /// In en, this message translates to:
  /// **'Real card numbers (issuer and checksum match), and security codes and expiry dates next to them.'**
  String get catCardHint;

  /// No description provided for @catIp.
  ///
  /// In en, this message translates to:
  /// **'Network addresses'**
  String get catIp;

  /// No description provided for @catIpHint.
  ///
  /// In en, this message translates to:
  /// **'IPv4, IPv6 and MAC addresses, except localhost.'**
  String get catIpHint;

  /// No description provided for @catSecret.
  ///
  /// In en, this message translates to:
  /// **'Passwords & API keys'**
  String get catSecret;

  /// No description provided for @catSecretHint.
  ///
  /// In en, this message translates to:
  /// **'More than 250 key formats, private keys, \"password: …\", passwords in links, cookies, webhook links, one-time codes and wallet recovery phrases.'**
  String get catSecretHint;

  /// No description provided for @catBirthdate.
  ///
  /// In en, this message translates to:
  /// **'Dates of birth'**
  String get catBirthdate;

  /// No description provided for @catBirthdateHint.
  ///
  /// In en, this message translates to:
  /// **'Dates next to words like \"born\" or \"geb.\".'**
  String get catBirthdateHint;

  /// No description provided for @catCrypto.
  ///
  /// In en, this message translates to:
  /// **'Crypto wallets'**
  String get catCrypto;

  /// No description provided for @catCryptoHint.
  ///
  /// In en, this message translates to:
  /// **'Bitcoin, Ethereum, Litecoin, Tron, Monero and Solana addresses.'**
  String get catCryptoHint;

  /// No description provided for @catLocation.
  ///
  /// In en, this message translates to:
  /// **'Locations'**
  String get catLocation;

  /// No description provided for @catLocationHint.
  ///
  /// In en, this message translates to:
  /// **'GPS coordinates and Plus Codes.'**
  String get catLocationHint;

  /// No description provided for @catVehicle.
  ///
  /// In en, this message translates to:
  /// **'Vehicles'**
  String get catVehicle;

  /// No description provided for @catVehicleHint.
  ///
  /// In en, this message translates to:
  /// **'Number plates and vehicle identification numbers.'**
  String get catVehicleHint;

  /// No description provided for @catCustom.
  ///
  /// In en, this message translates to:
  /// **'Your private terms'**
  String get catCustom;

  /// No description provided for @catCustomHint.
  ///
  /// In en, this message translates to:
  /// **'The words you list below.'**
  String get catCustomHint;

  /// No description provided for @catPerson.
  ///
  /// In en, this message translates to:
  /// **'Last names'**
  String get catPerson;

  /// No description provided for @catPersonHint.
  ///
  /// In en, this message translates to:
  /// **'First names stay, last names are hidden: \"Anna [LASTNAME_1]\".'**
  String get catPersonHint;

  /// No description provided for @catAddress.
  ///
  /// In en, this message translates to:
  /// **'Postal addresses'**
  String get catAddress;

  /// No description provided for @catAddressHint.
  ///
  /// In en, this message translates to:
  /// **'Street and number, postcode and town.'**
  String get catAddressHint;

  /// No description provided for @catOrganization.
  ///
  /// In en, this message translates to:
  /// **'Organizations'**
  String get catOrganization;

  /// No description provided for @catOrganizationHint.
  ///
  /// In en, this message translates to:
  /// **'Employers, clinics, schools – found by AI detection.'**
  String get catOrganizationHint;

  /// No description provided for @catId.
  ///
  /// In en, this message translates to:
  /// **'ID, tax and insurance numbers'**
  String get catId;

  /// No description provided for @catIdHint.
  ///
  /// In en, this message translates to:
  /// **'Steuer-ID, VAT IDs, ID cards, passports, social security and health insurance numbers from Germany, Europe, the UK and the US.'**
  String get catIdHint;

  /// No description provided for @privacyTermsTitle.
  ///
  /// In en, this message translates to:
  /// **'Your private terms'**
  String get privacyTermsTitle;

  /// No description provided for @privacyTermsHint.
  ///
  /// In en, this message translates to:
  /// **'Names, your company, your street – anything that should never reach a provider. One per line.'**
  String get privacyTermsHint;

  /// No description provided for @privacyTermsPlaceholder.
  ///
  /// In en, this message translates to:
  /// **'Anna Schmidt\nAcme Holding\nHauptstraße 5'**
  String get privacyTermsPlaceholder;

  /// No description provided for @privacyAllowTitle.
  ///
  /// In en, this message translates to:
  /// **'Never hide'**
  String get privacyAllowTitle;

  /// No description provided for @privacyAllowHint.
  ///
  /// In en, this message translates to:
  /// **'Values that may always go out, one per line. Start with @ to allow a whole mail domain, e.g. your company\'s.'**
  String get privacyAllowHint;

  /// No description provided for @privacyAllowPlaceholder.
  ///
  /// In en, this message translates to:
  /// **'@acme.example\nsupport@acme.example'**
  String get privacyAllowPlaceholder;

  /// No description provided for @privacyAiTitle.
  ///
  /// In en, this message translates to:
  /// **'AI detection'**
  String get privacyAiTitle;

  /// No description provided for @privacyAiHint.
  ///
  /// In en, this message translates to:
  /// **'Everything above is found on your server in about a millisecond, without AI. A fast AI model can also read each new message to find organizations and what the rules miss.'**
  String get privacyAiHint;

  /// No description provided for @privacyAiEnable.
  ///
  /// In en, this message translates to:
  /// **'Also look with AI'**
  String get privacyAiEnable;

  /// No description provided for @privacyAiEnableHint.
  ///
  /// In en, this message translates to:
  /// **'Adds about a second to new messages; repeated messages are remembered.'**
  String get privacyAiEnableHint;

  /// No description provided for @privacyAiModel.
  ///
  /// In en, this message translates to:
  /// **'Detection model'**
  String get privacyAiModel;

  /// No description provided for @privacyAiModelHint.
  ///
  /// In en, this message translates to:
  /// **'\"auto:fast\" picks the fastest model. Use a local model, e.g. \"ollama/…\", if no provider should see the raw text.'**
  String get privacyAiModelHint;

  /// No description provided for @privacyAiFreeOnly.
  ///
  /// In en, this message translates to:
  /// **'Use free models only for detection'**
  String get privacyAiFreeOnly;

  /// No description provided for @privacyAiWarning.
  ///
  /// In en, this message translates to:
  /// **'The detection model sees the original text of new messages. Pick a provider you trust or a local model.'**
  String get privacyAiWarning;

  /// No description provided for @privacyTryHint.
  ///
  /// In en, this message translates to:
  /// **'Paste a text to see exactly what a provider would receive. Nothing is sent to a model (except AI detection, if it is on).'**
  String get privacyTryHint;

  /// No description provided for @privacyTrySample.
  ///
  /// In en, this message translates to:
  /// **'Hi, I am Anna Schmidt, born 14.07.1988, living at Lindenstraße 12, 50674 Köln. Please send the invoice to anna.schmidt@example.de and call me at +49 170 1234567. My IBAN is DE89 3704 0044 0532 0130 00, my tax ID 86 095 742 719, the server password: hunter2pw.'**
  String get privacyTrySample;

  /// No description provided for @privacyTryResult.
  ///
  /// In en, this message translates to:
  /// **'{count} values hidden'**
  String privacyTryResult(String count);

  /// No description provided for @privacyTryFoundBy.
  ///
  /// In en, this message translates to:
  /// **'Found by: {list}'**
  String privacyTryFoundBy(String list);

  /// No description provided for @privacyFootnote.
  ///
  /// In en, this message translates to:
  /// **'Hidden values never leave your server and are never written to disk; the mapping lives only for the duration of one request. Detection is a strong safety net, not a guarantee – keep very sensitive data out of prompts.'**
  String get privacyFootnote;

  /// No description provided for @compressionSaved.
  ///
  /// In en, this message translates to:
  /// **'Tokens saved'**
  String get compressionSaved;

  /// No description provided for @compressionRequests.
  ///
  /// In en, this message translates to:
  /// **'Compressed requests'**
  String get compressionRequests;

  /// No description provided for @compressionLevel.
  ///
  /// In en, this message translates to:
  /// **'Level'**
  String get compressionLevel;

  /// No description provided for @compressionLevelHint.
  ///
  /// In en, this message translates to:
  /// **'Smaller prompts are cheaper, faster and fit more models. Higher levels save more but drop older details.'**
  String get compressionLevelHint;

  /// No description provided for @levelOff.
  ///
  /// In en, this message translates to:
  /// **'Off'**
  String get levelOff;

  /// No description provided for @levelOffHint.
  ///
  /// In en, this message translates to:
  /// **'Requests are sent exactly as your app wrote them.'**
  String get levelOffHint;

  /// No description provided for @levelLossless.
  ///
  /// In en, this message translates to:
  /// **'Lossless'**
  String get levelLossless;

  /// No description provided for @levelLosslessHint.
  ///
  /// In en, this message translates to:
  /// **'Removes extra whitespace, minifies JSON and drops duplicate blocks. The meaning never changes; code is left untouched.'**
  String get levelLosslessHint;

  /// No description provided for @levelBalanced.
  ///
  /// In en, this message translates to:
  /// **'Balanced'**
  String get levelBalanced;

  /// No description provided for @levelBalancedHint.
  ///
  /// In en, this message translates to:
  /// **'Lossless, plus: repeated log lines are collapsed, JSON lists become compact tables and very long tool outputs keep their start and end. Recommended for agents.'**
  String get levelBalancedHint;

  /// No description provided for @levelAggressive.
  ///
  /// In en, this message translates to:
  /// **'Aggressive'**
  String get levelAggressive;

  /// No description provided for @levelAggressiveHint.
  ///
  /// In en, this message translates to:
  /// **'Balanced, plus: older tool outputs are hidden and older long messages are shortened. Research shows this keeps agents about as accurate at half the cost. Your latest message is never cut.'**
  String get levelAggressiveHint;

  /// No description provided for @compressionEngines.
  ///
  /// In en, this message translates to:
  /// **'Steps'**
  String get compressionEngines;

  /// No description provided for @compressionEnginesHint.
  ///
  /// In en, this message translates to:
  /// **'Each step only applies when it actually makes the request smaller.'**
  String get compressionEnginesHint;

  /// No description provided for @compressionLimits.
  ///
  /// In en, this message translates to:
  /// **'When to compress'**
  String get compressionLimits;

  /// No description provided for @compressionMinTokens.
  ///
  /// In en, this message translates to:
  /// **'Start lossy steps from (tokens)'**
  String get compressionMinTokens;

  /// No description provided for @compressionMinTokensHint.
  ///
  /// In en, this message translates to:
  /// **'Shorter requests only get lossless clean-up.'**
  String get compressionMinTokensHint;

  /// No description provided for @compressionKeepRecent.
  ///
  /// In en, this message translates to:
  /// **'Recent messages kept whole'**
  String get compressionKeepRecent;

  /// No description provided for @compressionKeepRecentHint.
  ///
  /// In en, this message translates to:
  /// **'Older messages may be shortened, these never.'**
  String get compressionKeepRecentHint;

  /// No description provided for @compressionTarget.
  ///
  /// In en, this message translates to:
  /// **'Token target (optional)'**
  String get compressionTarget;

  /// No description provided for @compressionTargetHint.
  ///
  /// In en, this message translates to:
  /// **'If a request is still larger, the oldest messages are left out until it fits.'**
  String get compressionTargetHint;

  /// No description provided for @engWhitespace.
  ///
  /// In en, this message translates to:
  /// **'Clean up whitespace'**
  String get engWhitespace;

  /// No description provided for @engWhitespaceHint.
  ///
  /// In en, this message translates to:
  /// **'Trailing spaces and runs of empty lines, outside code.'**
  String get engWhitespaceHint;

  /// No description provided for @engJsonMinify.
  ///
  /// In en, this message translates to:
  /// **'Minify JSON'**
  String get engJsonMinify;

  /// No description provided for @engJsonMinifyHint.
  ///
  /// In en, this message translates to:
  /// **'Same data without indentation.'**
  String get engJsonMinifyHint;

  /// No description provided for @engDedupe.
  ///
  /// In en, this message translates to:
  /// **'Drop duplicate blocks'**
  String get engDedupe;

  /// No description provided for @engDedupeHint.
  ///
  /// In en, this message translates to:
  /// **'Large texts sent twice are kept once (the newest).'**
  String get engDedupeHint;

  /// No description provided for @engRepeatedLines.
  ///
  /// In en, this message translates to:
  /// **'Collapse repeated lines'**
  String get engRepeatedLines;

  /// No description provided for @engRepeatedLinesHint.
  ///
  /// In en, this message translates to:
  /// **'\"line repeated 40 more times\" instead of 40 identical log lines.'**
  String get engRepeatedLinesHint;

  /// No description provided for @engJsonTables.
  ///
  /// In en, this message translates to:
  /// **'JSON lists as tables'**
  String get engJsonTables;

  /// No description provided for @engJsonTablesHint.
  ///
  /// In en, this message translates to:
  /// **'Lists of objects with the same fields list each field name once.'**
  String get engJsonTablesHint;

  /// No description provided for @engTrimToolOutput.
  ///
  /// In en, this message translates to:
  /// **'Trim long tool output'**
  String get engTrimToolOutput;

  /// No description provided for @engTrimToolOutputHint.
  ///
  /// In en, this message translates to:
  /// **'Keeps the beginning and end, where results and errors usually are.'**
  String get engTrimToolOutputHint;

  /// No description provided for @engMaskOldToolOutput.
  ///
  /// In en, this message translates to:
  /// **'Hide older tool output'**
  String get engMaskOldToolOutput;

  /// No description provided for @engMaskOldToolOutputHint.
  ///
  /// In en, this message translates to:
  /// **'Only the last three tool results stay complete.'**
  String get engMaskOldToolOutputHint;

  /// No description provided for @engCondenseHistory.
  ///
  /// In en, this message translates to:
  /// **'Shorten older messages'**
  String get engCondenseHistory;

  /// No description provided for @engCondenseHistoryHint.
  ///
  /// In en, this message translates to:
  /// **'Long older messages keep their start and end.'**
  String get engCondenseHistoryHint;

  /// No description provided for @engTokenTarget.
  ///
  /// In en, this message translates to:
  /// **'Enforce the token target'**
  String get engTokenTarget;

  /// No description provided for @engTokenTargetHint.
  ///
  /// In en, this message translates to:
  /// **'Leaves out the oldest messages if needed.'**
  String get engTokenTargetHint;

  /// No description provided for @compressionTryHint.
  ///
  /// In en, this message translates to:
  /// **'Paste a long text or log to see how much the current level saves.'**
  String get compressionTryHint;

  /// No description provided for @compressionTryResult.
  ///
  /// In en, this message translates to:
  /// **'{before} → {after} tokens'**
  String compressionTryResult(String before, String after);

  /// No description provided for @tryItTitle.
  ///
  /// In en, this message translates to:
  /// **'Try it'**
  String get tryItTitle;

  /// No description provided for @tryItRun.
  ///
  /// In en, this message translates to:
  /// **'Show result'**
  String get tryItRun;

  /// No description provided for @tryItSent.
  ///
  /// In en, this message translates to:
  /// **'What a provider would receive'**
  String get tryItSent;

  /// No description provided for @pgEmbedHint.
  ///
  /// In en, this message translates to:
  /// **'One text per line. NeoRouter turns each into a vector and shows how similar they are.'**
  String get pgEmbedHint;

  /// No description provided for @pgEmbedRun.
  ///
  /// In en, this message translates to:
  /// **'Create vectors'**
  String get pgEmbedRun;

  /// No description provided for @pgEmbedResult.
  ///
  /// In en, this message translates to:
  /// **'{count} vectors with {dims} numbers each'**
  String pgEmbedResult(String count, String dims);

  /// No description provided for @pgSimilarity.
  ///
  /// In en, this message translates to:
  /// **'Similarity'**
  String get pgSimilarity;

  /// No description provided for @pgPinned.
  ///
  /// In en, this message translates to:
  /// **'This key now always uses {model} for embeddings, so your vectors stay comparable.'**
  String pgPinned(String model);

  /// No description provided for @pgImageHint.
  ///
  /// In en, this message translates to:
  /// **'Describe the picture you want.'**
  String get pgImageHint;

  /// No description provided for @pgImageRun.
  ///
  /// In en, this message translates to:
  /// **'Create image'**
  String get pgImageRun;

  /// No description provided for @pgSize.
  ///
  /// In en, this message translates to:
  /// **'Size'**
  String get pgSize;

  /// No description provided for @pgSpeechHint.
  ///
  /// In en, this message translates to:
  /// **'Type what should be read aloud.'**
  String get pgSpeechHint;

  /// No description provided for @pgSpeechRun.
  ///
  /// In en, this message translates to:
  /// **'Read aloud'**
  String get pgSpeechRun;

  /// No description provided for @pgVoice.
  ///
  /// In en, this message translates to:
  /// **'Voice'**
  String get pgVoice;

  /// No description provided for @pgVoiceDefault.
  ///
  /// In en, this message translates to:
  /// **'Default voice'**
  String get pgVoiceDefault;

  /// No description provided for @pgPlay.
  ///
  /// In en, this message translates to:
  /// **'Play'**
  String get pgPlay;

  /// No description provided for @pgStop.
  ///
  /// In en, this message translates to:
  /// **'Stop'**
  String get pgStop;

  /// No description provided for @pgDownload.
  ///
  /// In en, this message translates to:
  /// **'Download'**
  String get pgDownload;

  /// No description provided for @pgTranscribeHint.
  ///
  /// In en, this message translates to:
  /// **'Choose a recording (up to 25 MB) and NeoRouter turns it into text.'**
  String get pgTranscribeHint;

  /// No description provided for @pgChooseFile.
  ///
  /// In en, this message translates to:
  /// **'Choose audio file'**
  String get pgChooseFile;

  /// No description provided for @pgUseSpeech.
  ///
  /// In en, this message translates to:
  /// **'Use the spoken text'**
  String get pgUseSpeech;

  /// No description provided for @pgTranscribeRun.
  ///
  /// In en, this message translates to:
  /// **'Transcribe'**
  String get pgTranscribeRun;

  /// No description provided for @pgTranslate.
  ///
  /// In en, this message translates to:
  /// **'Translate to English'**
  String get pgTranslate;

  /// No description provided for @pgCheckHint.
  ///
  /// In en, this message translates to:
  /// **'Paste a message. A System One model decides in a split second whether it is safe.'**
  String get pgCheckHint;

  /// No description provided for @pgCheckRun.
  ///
  /// In en, this message translates to:
  /// **'Check'**
  String get pgCheckRun;

  /// No description provided for @pgSafe.
  ///
  /// In en, this message translates to:
  /// **'Looks safe'**
  String get pgSafe;

  /// No description provided for @pgCheckSafety.
  ///
  /// In en, this message translates to:
  /// **'Harmful content'**
  String get pgCheckSafety;

  /// No description provided for @pgCheckInjection.
  ///
  /// In en, this message translates to:
  /// **'Prompt attacks'**
  String get pgCheckInjection;

  /// No description provided for @pgFlagged.
  ///
  /// In en, this message translates to:
  /// **'Flagged'**
  String get pgFlagged;

  /// No description provided for @pgVideoHint.
  ///
  /// In en, this message translates to:
  /// **'Describe a short scene. Videos take a few minutes and are never free.'**
  String get pgVideoHint;

  /// No description provided for @pgVideoRun.
  ///
  /// In en, this message translates to:
  /// **'Create video'**
  String get pgVideoRun;

  /// No description provided for @pgSeconds.
  ///
  /// In en, this message translates to:
  /// **'{count} seconds'**
  String pgSeconds(String count);

  /// No description provided for @pgVideoStatus.
  ///
  /// In en, this message translates to:
  /// **'Status: {status}'**
  String pgVideoStatus(String status);

  /// No description provided for @pgNoModels.
  ///
  /// In en, this message translates to:
  /// **'None of your connections offers this kind of model yet.'**
  String get pgNoModels;

  /// No description provided for @pgAddProvider.
  ///
  /// In en, this message translates to:
  /// **'Add a provider'**
  String get pgAddProvider;

  /// No description provided for @unitPrice.
  ///
  /// In en, this message translates to:
  /// **'Price per unit (USD)'**
  String get unitPrice;

  /// No description provided for @unitImage.
  ///
  /// In en, this message translates to:
  /// **'per image'**
  String get unitImage;

  /// No description provided for @unitSecond.
  ///
  /// In en, this message translates to:
  /// **'per second'**
  String get unitSecond;

  /// No description provided for @unitMinute.
  ///
  /// In en, this message translates to:
  /// **'per minute of audio'**
  String get unitMinute;

  /// No description provided for @unitMchar.
  ///
  /// In en, this message translates to:
  /// **'per million characters'**
  String get unitMchar;

  /// No description provided for @unitSearch.
  ///
  /// In en, this message translates to:
  /// **'per search'**
  String get unitSearch;

  /// No description provided for @pgSearchHint.
  ///
  /// In en, this message translates to:
  /// **'Search the web through your search engines. Chat models use the same engines when a request turns web search on.'**
  String get pgSearchHint;

  /// No description provided for @pgSearchRun.
  ///
  /// In en, this message translates to:
  /// **'Search'**
  String get pgSearchRun;

  /// No description provided for @pgNoResults.
  ///
  /// In en, this message translates to:
  /// **'No results.'**
  String get pgNoResults;

  /// No description provided for @activityKind.
  ///
  /// In en, this message translates to:
  /// **'Kind'**
  String get activityKind;

  /// No description provided for @unitPriceLabel.
  ///
  /// In en, this message translates to:
  /// **'{price} {unit}'**
  String unitPriceLabel(String price, String unit);

  /// No description provided for @unitKind.
  ///
  /// In en, this message translates to:
  /// **'Billed'**
  String get unitKind;

  /// No description provided for @navTeam.
  ///
  /// In en, this message translates to:
  /// **'Team'**
  String get navTeam;

  /// No description provided for @teamTitle.
  ///
  /// In en, this message translates to:
  /// **'Team'**
  String get teamTitle;

  /// No description provided for @teamDescription.
  ///
  /// In en, this message translates to:
  /// **'Let other people help manage your NeoRouter. You decide per person what they may see and change. Everything they create stays yours.'**
  String get teamDescription;

  /// No description provided for @inviteSomeone.
  ///
  /// In en, this message translates to:
  /// **'Invite someone'**
  String get inviteSomeone;

  /// No description provided for @teamMembersTitle.
  ///
  /// In en, this message translates to:
  /// **'People in your team'**
  String get teamMembersTitle;

  /// No description provided for @teamMembersHint.
  ///
  /// In en, this message translates to:
  /// **'They work in your space with the rights you gave them.'**
  String get teamMembersHint;

  /// No description provided for @noMembersTitle.
  ///
  /// In en, this message translates to:
  /// **'Nobody here yet'**
  String get noMembersTitle;

  /// No description provided for @noMembersMessage.
  ///
  /// In en, this message translates to:
  /// **'Create an invite link and send it to the person. They need an account on this server.'**
  String get noMembersMessage;

  /// No description provided for @inviteLinksTitle.
  ///
  /// In en, this message translates to:
  /// **'Open invite links'**
  String get inviteLinksTitle;

  /// No description provided for @inviteLinksHint.
  ///
  /// In en, this message translates to:
  /// **'Anyone with an open link can join. Revoke links you no longer need.'**
  String get inviteLinksHint;

  /// No description provided for @joinTeamTitle.
  ///
  /// In en, this message translates to:
  /// **'Someone else\'s team'**
  String get joinTeamTitle;

  /// No description provided for @joinTeamLabel.
  ///
  /// In en, this message translates to:
  /// **'Got an invite link?'**
  String get joinTeamLabel;

  /// No description provided for @joinTeamHint.
  ///
  /// In en, this message translates to:
  /// **'Join to help manage that person\'s NeoRouter. Your own space stays as it is.'**
  String get joinTeamHint;

  /// No description provided for @useInviteLink.
  ///
  /// In en, this message translates to:
  /// **'Use invite link'**
  String get useInviteLink;

  /// No description provided for @memberOfTitle.
  ///
  /// In en, this message translates to:
  /// **'You are in {name}\'s team'**
  String memberOfTitle(String name);

  /// No description provided for @memberOfHint.
  ///
  /// In en, this message translates to:
  /// **'Switch spaces in the sidebar to work there.'**
  String get memberOfHint;

  /// No description provided for @youMay.
  ///
  /// In en, this message translates to:
  /// **'You may'**
  String get youMay;

  /// No description provided for @openSpaceOf.
  ///
  /// In en, this message translates to:
  /// **'Open {name}\'s space'**
  String openSpaceOf(String name);

  /// No description provided for @leaveTeam.
  ///
  /// In en, this message translates to:
  /// **'Leave team'**
  String get leaveTeam;

  /// No description provided for @leaveTeamConfirm.
  ///
  /// In en, this message translates to:
  /// **'Leave {name}\'s team? You lose access until they invite you again. What you created there stays.'**
  String leaveTeamConfirm(String name);

  /// No description provided for @editPermissions.
  ///
  /// In en, this message translates to:
  /// **'Change rights'**
  String get editPermissions;

  /// No description provided for @permissionsFor.
  ///
  /// In en, this message translates to:
  /// **'What {name} may do'**
  String permissionsFor(String name);

  /// No description provided for @removeMember.
  ///
  /// In en, this message translates to:
  /// **'Remove from team'**
  String get removeMember;

  /// No description provided for @removeMemberConfirm.
  ///
  /// In en, this message translates to:
  /// **'Remove {name}? They lose access right away. Keys and providers they created stay.'**
  String removeMemberConfirm(String name);

  /// No description provided for @noPermissions.
  ///
  /// In en, this message translates to:
  /// **'Nothing yet'**
  String get noPermissions;

  /// No description provided for @permissionCount.
  ///
  /// In en, this message translates to:
  /// **'{count} rights'**
  String permissionCount(String count);

  /// No description provided for @permView.
  ///
  /// In en, this message translates to:
  /// **'See'**
  String get permView;

  /// No description provided for @permCreate.
  ///
  /// In en, this message translates to:
  /// **'Create'**
  String get permCreate;

  /// No description provided for @permEdit.
  ///
  /// In en, this message translates to:
  /// **'Change'**
  String get permEdit;

  /// No description provided for @permDelete.
  ///
  /// In en, this message translates to:
  /// **'Delete'**
  String get permDelete;

  /// No description provided for @permActivityView.
  ///
  /// In en, this message translates to:
  /// **'See activity and costs'**
  String get permActivityView;

  /// No description provided for @permPlaygroundUse.
  ///
  /// In en, this message translates to:
  /// **'Use the playground'**
  String get permPlaygroundUse;

  /// No description provided for @permProvidersManage.
  ///
  /// In en, this message translates to:
  /// **'Add and change'**
  String get permProvidersManage;

  /// No description provided for @permModelsManage.
  ///
  /// In en, this message translates to:
  /// **'Turn models on or off'**
  String get permModelsManage;

  /// No description provided for @presetView.
  ///
  /// In en, this message translates to:
  /// **'Only look'**
  String get presetView;

  /// No description provided for @presetKeys.
  ///
  /// In en, this message translates to:
  /// **'Manage API keys'**
  String get presetKeys;

  /// No description provided for @presetFull.
  ///
  /// In en, this message translates to:
  /// **'Everything'**
  String get presetFull;

  /// No description provided for @inviteLabel.
  ///
  /// In en, this message translates to:
  /// **'Name'**
  String get inviteLabel;

  /// No description provided for @inviteLabelHint.
  ///
  /// In en, this message translates to:
  /// **'Who is this link for?'**
  String get inviteLabelHint;

  /// No description provided for @inviteExpiry.
  ///
  /// In en, this message translates to:
  /// **'Link works for'**
  String get inviteExpiry;

  /// No description provided for @expiry24h.
  ///
  /// In en, this message translates to:
  /// **'24 hours'**
  String get expiry24h;

  /// No description provided for @expiry7d.
  ///
  /// In en, this message translates to:
  /// **'7 days'**
  String get expiry7d;

  /// No description provided for @expiry30d.
  ///
  /// In en, this message translates to:
  /// **'30 days'**
  String get expiry30d;

  /// No description provided for @expiryNever.
  ///
  /// In en, this message translates to:
  /// **'Until revoked'**
  String get expiryNever;

  /// No description provided for @inviteSingleUseLabel.
  ///
  /// In en, this message translates to:
  /// **'One person only'**
  String get inviteSingleUseLabel;

  /// No description provided for @inviteSingleUseHint.
  ///
  /// In en, this message translates to:
  /// **'The link stops working once someone joins.'**
  String get inviteSingleUseHint;

  /// No description provided for @invitePermissionsLabel.
  ///
  /// In en, this message translates to:
  /// **'What they may do'**
  String get invitePermissionsLabel;

  /// No description provided for @createInviteLink.
  ///
  /// In en, this message translates to:
  /// **'Create link'**
  String get createInviteLink;

  /// No description provided for @inviteLinkTitle.
  ///
  /// In en, this message translates to:
  /// **'Invite link ready'**
  String get inviteLinkTitle;

  /// No description provided for @inviteLinkBody.
  ///
  /// In en, this message translates to:
  /// **'Send this link to the person. It is shown only now; you can revoke it any time.'**
  String get inviteLinkBody;

  /// No description provided for @inviteUntitled.
  ///
  /// In en, this message translates to:
  /// **'Invite link'**
  String get inviteUntitled;

  /// No description provided for @inviteSingleUse.
  ///
  /// In en, this message translates to:
  /// **'One person'**
  String get inviteSingleUse;

  /// No description provided for @inviteReusable.
  ///
  /// In en, this message translates to:
  /// **'Reusable · used {count}×'**
  String inviteReusable(String count);

  /// No description provided for @inviteNoExpiry.
  ///
  /// In en, this message translates to:
  /// **'No expiry'**
  String get inviteNoExpiry;

  /// No description provided for @inviteExpires.
  ///
  /// In en, this message translates to:
  /// **'Until {date}'**
  String inviteExpires(String date);

  /// No description provided for @inviteLinkField.
  ///
  /// In en, this message translates to:
  /// **'Invite link'**
  String get inviteLinkField;

  /// No description provided for @inviteLinkFieldHint.
  ///
  /// In en, this message translates to:
  /// **'Paste the link you got'**
  String get inviteLinkFieldHint;

  /// No description provided for @inviteCheck.
  ///
  /// In en, this message translates to:
  /// **'Check link'**
  String get inviteCheck;

  /// No description provided for @invitePreview.
  ///
  /// In en, this message translates to:
  /// **'Join {name}\'s team?'**
  String invitePreview(String name);

  /// No description provided for @invitePreviewHint.
  ///
  /// In en, this message translates to:
  /// **'You can then work in their space with these rights:'**
  String get invitePreviewHint;

  /// No description provided for @joinTeam.
  ///
  /// In en, this message translates to:
  /// **'Join team'**
  String get joinTeam;

  /// No description provided for @spaceLabel.
  ///
  /// In en, this message translates to:
  /// **'Space'**
  String get spaceLabel;

  /// No description provided for @mySpace.
  ///
  /// In en, this message translates to:
  /// **'My space'**
  String get mySpace;

  /// No description provided for @spaceOf.
  ///
  /// In en, this message translates to:
  /// **'{name}\'s space'**
  String spaceOf(String name);

  /// No description provided for @switchSpace.
  ///
  /// In en, this message translates to:
  /// **'Switch space'**
  String get switchSpace;

  /// No description provided for @createdBy.
  ///
  /// In en, this message translates to:
  /// **'Created by {name}'**
  String createdBy(String name);

  /// No description provided for @formerMember.
  ///
  /// In en, this message translates to:
  /// **'a former member'**
  String get formerMember;

  /// No description provided for @inTeamOf.
  ///
  /// In en, this message translates to:
  /// **'in {name}\'s team'**
  String inTeamOf(String name);

  /// No description provided for @teamActivityTitle.
  ///
  /// In en, this message translates to:
  /// **'Team activity'**
  String get teamActivityTitle;

  /// No description provided for @teamActivityHint.
  ///
  /// In en, this message translates to:
  /// **'Invites, joins and changed rights across the server.'**
  String get teamActivityHint;

  /// No description provided for @teamActivityEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing has happened yet.'**
  String get teamActivityEmpty;

  /// No description provided for @teamEventInviteCreate.
  ///
  /// In en, this message translates to:
  /// **'{actor} created an invite link'**
  String teamEventInviteCreate(String actor);

  /// No description provided for @teamEventInviteRevoke.
  ///
  /// In en, this message translates to:
  /// **'{actor} revoked an invite link'**
  String teamEventInviteRevoke(String actor);

  /// No description provided for @teamEventJoin.
  ///
  /// In en, this message translates to:
  /// **'{actor} joined {subject}\'s team'**
  String teamEventJoin(String actor, String subject);

  /// No description provided for @teamEventPermissions.
  ///
  /// In en, this message translates to:
  /// **'{actor} changed what {subject} may do'**
  String teamEventPermissions(String actor, String subject);

  /// No description provided for @teamEventRelease.
  ///
  /// In en, this message translates to:
  /// **'{actor} removed {subject} from the team'**
  String teamEventRelease(String actor, String subject);

  /// No description provided for @teamEventLeave.
  ///
  /// In en, this message translates to:
  /// **'{actor} left {subject}\'s team'**
  String teamEventLeave(String actor, String subject);

  /// No description provided for @homeCapacityTitle.
  ///
  /// In en, this message translates to:
  /// **'Around the clock'**
  String get homeCapacityTitle;

  /// No description provided for @homeCapacityValue.
  ///
  /// In en, this message translates to:
  /// **'{count} requests a day'**
  String homeCapacityValue(String count);

  /// No description provided for @homeCapacityCaption.
  ///
  /// In en, this message translates to:
  /// **'What your free models could answer if used non-stop: about {hour} an hour, from the usage limits of {providers} providers.'**
  String homeCapacityCaption(String hour, String providers);

  /// No description provided for @homeCapacityUnknown.
  ///
  /// In en, this message translates to:
  /// **'{models} more free models publish no limit and are not counted.'**
  String homeCapacityUnknown(String models);

  /// No description provided for @homeCapacityNone.
  ///
  /// In en, this message translates to:
  /// **'None of your free models has a known usage limit yet, so there is nothing to add up.'**
  String get homeCapacityNone;

  /// No description provided for @keyLimitsTitle.
  ///
  /// In en, this message translates to:
  /// **'Limits'**
  String get keyLimitsTitle;

  /// No description provided for @keyNoLimitHint.
  ///
  /// In en, this message translates to:
  /// **'Leave a field empty for no limit.'**
  String get keyNoLimitHint;

  /// No description provided for @keyRateLimit.
  ///
  /// In en, this message translates to:
  /// **'Rate limit'**
  String get keyRateLimit;

  /// No description provided for @keyRequestsPerMinute.
  ///
  /// In en, this message translates to:
  /// **'requests per minute'**
  String get keyRequestsPerMinute;

  /// No description provided for @keyTokenLimit.
  ///
  /// In en, this message translates to:
  /// **'Token limit'**
  String get keyTokenLimit;

  /// No description provided for @keyTokensPer.
  ///
  /// In en, this message translates to:
  /// **'tokens per'**
  String get keyTokensPer;

  /// No description provided for @keyTokenHint.
  ///
  /// In en, this message translates to:
  /// **'Counts the tokens used on free and subscription models. Once reached, the key waits until it resets.'**
  String get keyTokenHint;

  /// No description provided for @keyTokenFallsBack.
  ///
  /// In en, this message translates to:
  /// **'Counts the tokens used on free and subscription models. Once reached, paid models take over within the budget.'**
  String get keyTokenFallsBack;

  /// No description provided for @tokensUsedOf.
  ///
  /// In en, this message translates to:
  /// **'{used} of {limit} tokens'**
  String tokensUsedOf(String used, String limit);

  /// No description provided for @styleKindsTitle.
  ///
  /// In en, this message translates to:
  /// **'Which models'**
  String get styleKindsTitle;

  /// No description provided for @styleKindsHint.
  ///
  /// In en, this message translates to:
  /// **'\"If needed\" models step in only when the preferred ones cannot answer.'**
  String get styleKindsHint;

  /// No description provided for @styleKindPrefer.
  ///
  /// In en, this message translates to:
  /// **'Prefer'**
  String get styleKindPrefer;

  /// No description provided for @styleKindIfNeeded.
  ///
  /// In en, this message translates to:
  /// **'If needed'**
  String get styleKindIfNeeded;

  /// No description provided for @styleKindNever.
  ///
  /// In en, this message translates to:
  /// **'Never'**
  String get styleKindNever;

  /// No description provided for @stylePrioritiesTitle.
  ///
  /// In en, this message translates to:
  /// **'What matters most'**
  String get stylePrioritiesTitle;

  /// No description provided for @stylePriorityQuality.
  ///
  /// In en, this message translates to:
  /// **'Quality'**
  String get stylePriorityQuality;

  /// No description provided for @stylePriorityQualityHint.
  ///
  /// In en, this message translates to:
  /// **'Smarter, more capable answers'**
  String get stylePriorityQualityHint;

  /// No description provided for @stylePrioritySpeed.
  ///
  /// In en, this message translates to:
  /// **'Speed'**
  String get stylePrioritySpeed;

  /// No description provided for @stylePrioritySpeedHint.
  ///
  /// In en, this message translates to:
  /// **'Answers start sooner and finish faster'**
  String get stylePrioritySpeedHint;

  /// No description provided for @stylePriorityReliability.
  ///
  /// In en, this message translates to:
  /// **'Reliability'**
  String get stylePriorityReliability;

  /// No description provided for @stylePriorityReliabilityHint.
  ///
  /// In en, this message translates to:
  /// **'Models that rarely fail or time out'**
  String get stylePriorityReliabilityHint;

  /// No description provided for @stylePriorityCost.
  ///
  /// In en, this message translates to:
  /// **'Saving money'**
  String get stylePriorityCost;

  /// No description provided for @stylePriorityCostHint.
  ///
  /// In en, this message translates to:
  /// **'Cheaper models, free ones first'**
  String get stylePriorityCostHint;

  /// No description provided for @styleLevel0.
  ///
  /// In en, this message translates to:
  /// **'Not important'**
  String get styleLevel0;

  /// No description provided for @styleLevel1.
  ///
  /// In en, this message translates to:
  /// **'A little'**
  String get styleLevel1;

  /// No description provided for @styleLevel2.
  ///
  /// In en, this message translates to:
  /// **'Important'**
  String get styleLevel2;

  /// No description provided for @styleLevel3.
  ///
  /// In en, this message translates to:
  /// **'Very important'**
  String get styleLevel3;

  /// No description provided for @styleMoreTitle.
  ///
  /// In en, this message translates to:
  /// **'Fine-tuning'**
  String get styleMoreTitle;

  /// No description provided for @styleQualityFloor.
  ///
  /// In en, this message translates to:
  /// **'Lowest quality allowed'**
  String get styleQualityFloor;

  /// No description provided for @styleQualityAny.
  ///
  /// In en, this message translates to:
  /// **'Any model'**
  String get styleQualityAny;

  /// No description provided for @styleQualityDecent.
  ///
  /// In en, this message translates to:
  /// **'Decent and up'**
  String get styleQualityDecent;

  /// No description provided for @styleQualityGood.
  ///
  /// In en, this message translates to:
  /// **'Good and up'**
  String get styleQualityGood;

  /// No description provided for @styleQualityTop.
  ///
  /// In en, this message translates to:
  /// **'Top models only'**
  String get styleQualityTop;

  /// No description provided for @stylePriceCap.
  ///
  /// In en, this message translates to:
  /// **'Price limit for paid models'**
  String get stylePriceCap;

  /// No description provided for @stylePriceAny.
  ///
  /// In en, this message translates to:
  /// **'Any price'**
  String get stylePriceAny;

  /// No description provided for @stylePricePremium.
  ///
  /// In en, this message translates to:
  /// **'Up to premium models'**
  String get stylePricePremium;

  /// No description provided for @stylePriceMid.
  ///
  /// In en, this message translates to:
  /// **'Mid-priced and below'**
  String get stylePriceMid;

  /// No description provided for @stylePriceCheap.
  ///
  /// In en, this message translates to:
  /// **'Cheap models only'**
  String get stylePriceCheap;

  /// No description provided for @stylePriceVeryCheap.
  ///
  /// In en, this message translates to:
  /// **'Very cheap only'**
  String get stylePriceVeryCheap;

  /// No description provided for @stylePriceDetail.
  ///
  /// In en, this message translates to:
  /// **'Answers cost at most {price} per 750,000 words'**
  String stylePriceDetail(String price);

  /// No description provided for @styleVariety.
  ///
  /// In en, this message translates to:
  /// **'Variety'**
  String get styleVariety;

  /// No description provided for @styleVarietyLow.
  ///
  /// In en, this message translates to:
  /// **'Stick to the best model'**
  String get styleVarietyLow;

  /// No description provided for @styleVarietyMid.
  ///
  /// In en, this message translates to:
  /// **'Spread over a few good models'**
  String get styleVarietyMid;

  /// No description provided for @styleVarietyHigh.
  ///
  /// In en, this message translates to:
  /// **'Spread widely'**
  String get styleVarietyHigh;

  /// No description provided for @styleAdaptive.
  ///
  /// In en, this message translates to:
  /// **'Stronger models for hard requests'**
  String get styleAdaptive;

  /// No description provided for @styleAdaptiveHint.
  ///
  /// In en, this message translates to:
  /// **'Long, code-heavy or tool requests lean towards smarter models.'**
  String get styleAdaptiveHint;

  /// No description provided for @styleSkipProviders.
  ///
  /// In en, this message translates to:
  /// **'Leave out'**
  String get styleSkipProviders;

  /// No description provided for @styleSkipProvidersHint.
  ///
  /// In en, this message translates to:
  /// **'Connections this style never uses.'**
  String get styleSkipProvidersHint;

  /// No description provided for @pgLiveHint.
  ///
  /// In en, this message translates to:
  /// **'Live voice is a spoken conversation in real time over a WebSocket. Apps such as NeoAgent connect to the address for the protocol they speak, with a router key; NeoRouter picks the model and falls back if a provider refuses.'**
  String get pgLiveHint;

  /// No description provided for @pgLiveEndpoints.
  ///
  /// In en, this message translates to:
  /// **'Where apps connect'**
  String get pgLiveEndpoints;

  /// No description provided for @pgLiveModelHint.
  ///
  /// In en, this message translates to:
  /// **'Use \"auto\" as the model, or one of the models below.'**
  String get pgLiveModelHint;

  /// No description provided for @pgLiveModels.
  ///
  /// In en, this message translates to:
  /// **'Live models'**
  String get pgLiveModels;

  /// No description provided for @pgLiveNoModels.
  ///
  /// In en, this message translates to:
  /// **'No live models yet. Gemini (AI Studio) and OpenAI connections bring them.'**
  String get pgLiveNoModels;

  /// No description provided for @connectTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect {name}?'**
  String connectTitle(String name);

  /// No description provided for @connectSubtitle.
  ///
  /// In en, this message translates to:
  /// **'{name} wants its own key for your NeoRouter. You choose what it may use and can disconnect it any time under API keys.'**
  String connectSubtitle(String name);

  /// No description provided for @connectSendsTo.
  ///
  /// In en, this message translates to:
  /// **'The key goes to {host}'**
  String connectSendsTo(String host);

  /// No description provided for @connectInsecure.
  ///
  /// In en, this message translates to:
  /// **'This address is not encrypted. Only continue if it is your own device or network.'**
  String get connectInsecure;

  /// No description provided for @connectDeviceHint.
  ///
  /// In en, this message translates to:
  /// **'The app is waiting on another device and picks up the key by itself.'**
  String get connectDeviceHint;

  /// No description provided for @connectWantsToUse.
  ///
  /// In en, this message translates to:
  /// **'It says it will use'**
  String get connectWantsToUse;

  /// No description provided for @connectInSpace.
  ///
  /// In en, this message translates to:
  /// **'The key is created in {name}.'**
  String connectInSpace(String name);

  /// No description provided for @connectApprove.
  ///
  /// In en, this message translates to:
  /// **'Connect'**
  String get connectApprove;

  /// No description provided for @connectDeny.
  ///
  /// In en, this message translates to:
  /// **'Decline'**
  String get connectDeny;

  /// No description provided for @connectCodeTitle.
  ///
  /// In en, this message translates to:
  /// **'Connect an app'**
  String get connectCodeTitle;

  /// No description provided for @connectCodeSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Enter the code the app shows you.'**
  String get connectCodeSubtitle;

  /// No description provided for @connectCodeField.
  ///
  /// In en, this message translates to:
  /// **'Code'**
  String get connectCodeField;

  /// No description provided for @connectCodeContinue.
  ///
  /// In en, this message translates to:
  /// **'Continue'**
  String get connectCodeContinue;

  /// No description provided for @connectDoneTitle.
  ///
  /// In en, this message translates to:
  /// **'{name} is connected'**
  String connectDoneTitle(String name);

  /// No description provided for @connectDoneBody.
  ///
  /// In en, this message translates to:
  /// **'You can go back to the app. To disconnect it later, revoke its key under API keys.'**
  String get connectDoneBody;

  /// No description provided for @connectDeniedTitle.
  ///
  /// In en, this message translates to:
  /// **'Declined'**
  String get connectDeniedTitle;

  /// No description provided for @connectDeniedBody.
  ///
  /// In en, this message translates to:
  /// **'{name} did not get a key.'**
  String connectDeniedBody(String name);

  /// No description provided for @connectFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'This connection did not work'**
  String get connectFailedTitle;

  /// No description provided for @connectOpenNeoRouter.
  ///
  /// In en, this message translates to:
  /// **'Open NeoRouter'**
  String get connectOpenNeoRouter;

  /// No description provided for @connectNoPermission.
  ///
  /// In en, this message translates to:
  /// **'The team owner has not allowed you to create keys here. Switch to your own space to connect the app.'**
  String get connectNoPermission;

  /// No description provided for @connectedAppsTitle.
  ///
  /// In en, this message translates to:
  /// **'Connected apps'**
  String get connectedAppsTitle;

  /// No description provided for @connectedAppsHint.
  ///
  /// In en, this message translates to:
  /// **'Apps that signed in with NeoRouter. Revoke one to disconnect it.'**
  String get connectedAppsHint;

  /// No description provided for @connectedVia.
  ///
  /// In en, this message translates to:
  /// **'Connected · {host}'**
  String connectedVia(String host);

  /// No description provided for @connectedApp.
  ///
  /// In en, this message translates to:
  /// **'Connected app'**
  String get connectedApp;
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
