// ignore: unused_import
import 'package:intl/intl.dart' as intl;
import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for German (`de`).
class AppL10nDe extends AppL10n {
  AppL10nDe([String locale = 'de']) : super(locale);

  @override
  String get appName => 'NeoRouter';

  @override
  String get brandTagline => 'INFERENCE ROUTER';

  @override
  String get cancel => 'Abbrechen';

  @override
  String get save => 'Speichern';

  @override
  String get delete => 'Löschen';

  @override
  String get close => 'Schließen';

  @override
  String get create => 'Erstellen';

  @override
  String get edit => 'Bearbeiten';

  @override
  String get retry => 'Erneut versuchen';

  @override
  String get refresh => 'Aktualisieren';

  @override
  String get copy => 'Kopieren';

  @override
  String get copied => 'In die Zwischenablage kopiert.';

  @override
  String get saved => 'Gespeichert.';

  @override
  String get enabled => 'Aktiv';

  @override
  String get disabled => 'Deaktiviert';

  @override
  String get never => 'nie';

  @override
  String get unlimited => 'Unbegrenzt';

  @override
  String get search => 'Suchen';

  @override
  String get free => 'Gratis';

  @override
  String get paid => 'Bezahlt';

  @override
  String get unknown => 'Unbekannt';

  @override
  String get logout => 'Abmelden';

  @override
  String get serverUnreachable => 'Der Server ist nicht erreichbar.';

  @override
  String get groupOverview => 'Überblick';

  @override
  String get groupSetup => 'Einrichtung';

  @override
  String get groupRouting => 'Routing';

  @override
  String get navDashboard => 'Dashboard';

  @override
  String get navProviders => 'Provider';

  @override
  String get navModels => 'Modelle';

  @override
  String get navKeys => 'Router-Keys';

  @override
  String get navRouting => 'Auto-Router';

  @override
  String get navQuotas => 'Kontingente';

  @override
  String get navLogs => 'Request-Log';

  @override
  String get navPlayground => 'Playground';

  @override
  String get navSettings => 'Einstellungen';

  @override
  String get authWelcome => 'Willkommen zurück';

  @override
  String get authCreate => 'Konto erstellen';

  @override
  String get authBootstrapTitle => 'NeoRouter einrichten';

  @override
  String get authLoginSubtitle => 'Melde dich an, um fortzufahren.';

  @override
  String get authRegisterSubtitle =>
      'Self-hosted. Deine Provider-Keys bleiben auf deinem Server.';

  @override
  String get authBootstrapSubtitle => 'Das erste Konto wird Administrator.';

  @override
  String get username => 'Benutzername';

  @override
  String get usernameOrEmail => 'Benutzername oder E-Mail';

  @override
  String get email => 'E-Mail';

  @override
  String get password => 'Passwort';

  @override
  String get confirmPassword => 'Passwort bestätigen';

  @override
  String get signIn => 'Anmelden';

  @override
  String get createAccount => 'Konto erstellen';

  @override
  String get toRegister => 'Noch kein Konto? Registrieren';

  @override
  String get toLogin => 'Schon ein Konto? Anmelden';

  @override
  String get forgotPassword => 'Passwort vergessen?';

  @override
  String get forgotPasswordTitle => 'Passwort zurücksetzen';

  @override
  String get forgotPasswordBody =>
      'Ein Administrator kann es auf dem Server zurücksetzen mit: neorouter reset-password <benutzer> <neues-passwort>';

  @override
  String get passwordsDontMatch => 'Die Passwörter stimmen nicht überein.';

  @override
  String get passwordTooShort =>
      'Verwende ein Passwort mit mindestens 12 Zeichen.';

  @override
  String get registrationClosed =>
      'Die Registrierung ist geschlossen. Bitte einen Administrator, sie zu öffnen.';

  @override
  String get dashboardEyebrow => 'Überblick';

  @override
  String get dashboardTitle => 'Dashboard';

  @override
  String get dashboardDescription =>
      'Traffic, Kosten und Zustand über alle verbundenen Provider.';

  @override
  String get range24h => '24 Std.';

  @override
  String get range7d => '7 Tage';

  @override
  String get range30d => '30 Tage';

  @override
  String get statRequests => 'Requests';

  @override
  String get statSuccessRate => 'Erfolgsquote';

  @override
  String get statCost => 'Ausgegeben';

  @override
  String get statTokens => 'Tokens';

  @override
  String get statFreeShare => 'Gratis bedient';

  @override
  String get statLatency => 'Ø Latenz';

  @override
  String get statFallbacks => 'Mit Fallback';

  @override
  String statInOut(String input, String output) {
    return '$input rein · $output raus';
  }

  @override
  String get topModels => 'Top-Modelle';

  @override
  String requestsShort(String count) {
    return '$count Req.';
  }

  @override
  String get trafficTitle => 'Traffic';

  @override
  String get errorsTitle => 'Fehler';

  @override
  String get noTrafficTitle => 'Noch kein Traffic';

  @override
  String get noTrafficMessage =>
      'Sende einen Request an deinen Endpoint – er erscheint dann hier.';

  @override
  String get endpointTitle => 'Dein Endpoint';

  @override
  String get endpointHint =>
      'OpenAI-, Anthropic- und Responses-kompatibel. Authentifizierung mit einem Router-Key.';

  @override
  String get quickStartTitle => 'Los geht’s';

  @override
  String get quickStartProviders =>
      'Provider hinzufügen – der Key wird geprüft und alle Modelle importiert.';

  @override
  String get quickStartKeys =>
      'Router-Key mit Budget oder Nur-Gratis-Modus erstellen.';

  @override
  String get quickStartUse =>
      'Modell „auto“ oder „provider/modell“ aus jedem OpenAI- oder Anthropic-SDK verwenden.';

  @override
  String get homeAllGood => 'Alles läuft rund';

  @override
  String get homeNeedsAttention => 'Ein Provider braucht deine Aufmerksamkeit';

  @override
  String homeHealthyOf(String healthy, String total) {
    return '$healthy von $total Providern funktionieren';
  }

  @override
  String get homeOpenPlayground => 'Playground öffnen';

  @override
  String get homeProblemHint =>
      'Anfragen überspringen ihn, bis das behoben ist. Sonst ist nichts betroffen.';

  @override
  String get homeFix => 'Beheben';

  @override
  String get homeConnectTitle => 'App verbinden';

  @override
  String get homeConnectHint =>
      'Funktioniert mit jedem OpenAI- oder Anthropic-SDK. Frag nach „auto“ und NeoRouter wählt das Modell.';

  @override
  String homeKeyHint(String name, String prefix) {
    return 'Nutze einen Router-Key, z. B. $name ($prefix…).';
  }

  @override
  String get homeManage => 'Verwalten';

  @override
  String get homeRecent => 'Letzte Anfragen';

  @override
  String get homeAllActivity => 'Alle Aktivität';

  @override
  String get homeNoRequests =>
      'Noch keine Anfragen. Schick eine aus dem Playground oder deiner App.';

  @override
  String homeQuotaLeft(String percent) {
    return '$percent % Kontingent übrig';
  }

  @override
  String homeResetsIn(String time) {
    return 'zurück in $time';
  }

  @override
  String get homeOk => 'OK';

  @override
  String get homeRetried => 'Wiederholt';

  @override
  String get providersEyebrow => 'Einrichtung';

  @override
  String get providersTitle => 'Provider';

  @override
  String get providersDescription =>
      'Verbinde beliebige Inference-APIs. Keys werden geprüft, verschlüsselt gespeichert und ihre Modelle automatisch importiert.';

  @override
  String get addProvider => 'Provider hinzufügen';

  @override
  String get chooseProvider => 'Provider auswählen';

  @override
  String get searchProviders => 'Provider suchen…';

  @override
  String get categoryFree => 'Gratis-Kontingente';

  @override
  String get categoryAggregator => 'Aggregatoren';

  @override
  String get categoryPaid => 'Bezahl-APIs';

  @override
  String get categoryLocal => 'Lokal';

  @override
  String get categoryCustom => 'Eigene Endpoints';

  @override
  String get categoryCommunity => 'Community';

  @override
  String get apiKey => 'API-Key';

  @override
  String get apiKeyOptional => 'API-Key (optional)';

  @override
  String get apiKeyKeep =>
      'API-Key (leer lassen, um den aktuellen zu behalten)';

  @override
  String get baseUrl => 'Base-URL';

  @override
  String get displayNameLabel => 'Anzeigename';

  @override
  String get slugLabel => 'Name in Modell-IDs';

  @override
  String slugHint(String slug) {
    return 'Modelle heißen dann $slug/<modell>.';
  }

  @override
  String get billingMode => 'Abrechnung';

  @override
  String get billingFree => 'Gratis-Kontingent – keine Zahlungsmethode';

  @override
  String get billingPaid => 'Bezahlter Account';

  @override
  String get billingFreeWarning =>
      'Wähle das Gratis-Kontingent nur, wenn dieser Account nichts kosten kann. NeoRouter behandelt dann alle Modelle dieses Accounts als 100 % gratis.';

  @override
  String get billingAlwaysFreeNote =>
      'Einige Modelle dieses Providers sind unabhängig von der Abrechnung immer gratis.';

  @override
  String get getKey => 'Key holen';

  @override
  String get checkAndAdd => 'Key prüfen & Modelle importieren';

  @override
  String providerAdded(String count) {
    return 'Key geprüft – $count Modelle importiert.';
  }

  @override
  String providerAddedUnverified(String message) {
    return 'Gespeichert. Der Key konnte noch nicht geprüft werden: $message';
  }

  @override
  String get statusHealthy => 'Funktioniert';

  @override
  String get statusInvalid => 'Key abgelehnt';

  @override
  String get statusError => 'Prüfung fehlgeschlagen';

  @override
  String get statusUnknown => 'Nicht geprüft';

  @override
  String get statusDisabled => 'Deaktiviert';

  @override
  String modelsCount(String total, String free) {
    return '$total Modelle · $free gratis';
  }

  @override
  String get revalidate => 'Key prüfen';

  @override
  String get syncModels => 'Modelle aktualisieren';

  @override
  String get probeQuota => 'Guthaben prüfen';

  @override
  String get removeProvider => 'Provider entfernen';

  @override
  String removeProviderConfirm(String name) {
    return '$name entfernen? Seine Modelle verschwinden aus dem Routing; die Request-Historie bleibt erhalten.';
  }

  @override
  String get editProvider => 'Provider bearbeiten';

  @override
  String lastChecked(String time) {
    return 'Geprüft $time';
  }

  @override
  String syncedModels(String count) {
    return '$count Modelle synchronisiert.';
  }

  @override
  String get noProvidersTitle => 'Noch keine Provider';

  @override
  String get noProvidersMessage =>
      'Füge deinen ersten Provider hinzu, um den Modellkatalog zu füllen.';

  @override
  String quotaProbeResult(String metric, String remaining) {
    return '$metric: $remaining';
  }

  @override
  String get quotaProbeEmpty => 'Dieser Provider meldet kein Guthaben.';

  @override
  String get modelsEyebrow => 'Katalog';

  @override
  String get modelsTitle => 'Modelle';

  @override
  String get modelsDescription =>
      'Alle Chat-Modelle deiner Provider, mit gelernter Geschwindigkeit und Zuverlässigkeit.';

  @override
  String get searchModels => 'Modelle suchen…';

  @override
  String get filterAll => 'Alle';

  @override
  String get filterFree => 'Gratis';

  @override
  String get filterEnabled => 'Aktiv';

  @override
  String get filterTools => 'Tools';

  @override
  String get filterVision => 'Vision';

  @override
  String get filterIssues => 'Probleme';

  @override
  String get intelligence => 'Intelligenz';

  @override
  String get speed => 'Speed';

  @override
  String get reliability => 'Zuverlässigkeit';

  @override
  String get cost => 'Kosten';

  @override
  String get price => 'Preis';

  @override
  String pricePerMillion(String input, String output) {
    return '$input / $output pro 1 Mio.';
  }

  @override
  String get priceUnknown => 'Preis unbekannt';

  @override
  String contextWindow(String tokens) {
    return '$tokens Kontext';
  }

  @override
  String get tools => 'Tools';

  @override
  String get vision => 'Vision';

  @override
  String get reasoning => 'Reasoning';

  @override
  String get freeReasonManualFree => 'Von dir als gratis markiert';

  @override
  String get freeReasonManualPaid => 'Von dir als bezahlt markiert';

  @override
  String get freeReasonProviderFreeModel => 'Gratis-Modell dieses Providers';

  @override
  String get freeReasonListedZeroPrice => 'Provider listet Preis 0';

  @override
  String get freeReasonFreeTierAccount => 'Gratis-Account';

  @override
  String get freeReasonPaid => 'Bezahlt';

  @override
  String get freeReasonPriceUnknown =>
      'Preis unbekannt – zählt nicht als gratis';

  @override
  String get freeReasonBilledObserved =>
      'Kostenpflichtig: der Provider hat dafür etwas berechnet';

  @override
  String get freeReasonNoFreeTier =>
      'Kostenpflichtig: kein Gratis-Kontingent auf diesem Konto';

  @override
  String get freeReasonNotInFreeTier =>
      'Kostenpflichtig: nicht im Gratis-Tarif enthalten';

  @override
  String get freeReasonBillingActive =>
      'Kostenpflichtig: Abrechnung auf diesem Konto aktiv';

  @override
  String get freeReasonBillingUnverified =>
      'Nicht gratis, bis der Gratis-Tarif bestätigt ist';

  @override
  String get billingVerifiedFree => 'Gratis-Tarif bestätigt';

  @override
  String get billingDetectedPaid => 'Abrechnung aktiv';

  @override
  String get billingUnverified => 'Gratis-Tarif unbestätigt';

  @override
  String get modelSettings => 'Modell-Einstellungen';

  @override
  String get overrideFree => 'Gratis-Status';

  @override
  String get overrideAuto => 'Automatisch';

  @override
  String get overrideForceFree => 'Immer gratis';

  @override
  String get overrideForcePaid => 'Immer bezahlt';

  @override
  String get inputPrice => 'Input USD / 1 Mio.';

  @override
  String get outputPrice => 'Output USD / 1 Mio.';

  @override
  String get intelligenceOverride =>
      'Intelligenz überschreiben (0–1, leer = automatisch)';

  @override
  String intelligenceSource(String source) {
    return 'Quelle: $source';
  }

  @override
  String get resetHealth => 'Zustand zurücksetzen';

  @override
  String get healthReset => 'Strafpunkte und Cooldowns zurückgesetzt.';

  @override
  String get enableAll => 'Alle aktivieren';

  @override
  String get disableAll => 'Alle deaktivieren';

  @override
  String get enableFreeOnly => 'Nur gratis aktivieren';

  @override
  String measured(String latency, String tps, String samples) {
    return '$latency · $tps tok/s · $samples Aufrufe';
  }

  @override
  String get notMeasured => 'Noch nicht gemessen';

  @override
  String penaltyLabel(String value) {
    return 'Strafpunkte $value';
  }

  @override
  String unavailableUntil(String reason, String time) {
    return '$reason bis $time';
  }

  @override
  String get noModelsTitle => 'Keine Modelle';

  @override
  String get noModelsMessage =>
      'Füge einen Provider hinzu oder ändere den Filter.';

  @override
  String modelsShown(String count) {
    return '$count Modelle';
  }

  @override
  String get keysEyebrow => 'Zugriff';

  @override
  String get keysTitle => 'Router-Keys';

  @override
  String get keysDescription =>
      'Keys für deine Apps. Jeder Key hat eigenen Modus, eigenes Budget und eigene Limits.';

  @override
  String get createKey => 'Key erstellen';

  @override
  String get editKey => 'Key bearbeiten';

  @override
  String get keyName => 'Name';

  @override
  String get keyMode => 'Modellzugriff';

  @override
  String get modeFreeOnly => 'Nur 100 % gratis Modelle';

  @override
  String get modePaid => 'Alle Modelle, Stopp beim Budget';

  @override
  String get modeBoth =>
      'Alle Modelle, danach nur gratis wenn das Budget aufgebraucht ist';

  @override
  String get budget => 'Budget (USD, leer = unbegrenzt)';

  @override
  String get budgetPeriod => 'Budget-Zeitraum';

  @override
  String get periodTotal => 'Gesamt';

  @override
  String get periodMonthly => 'Pro Monat';

  @override
  String get periodDaily => 'Pro Tag';

  @override
  String get rpmLimit => 'Requests pro Minute (leer = unbegrenzt)';

  @override
  String get allowedModels =>
      'Erlaubte Modelle – Glob-Muster, eines pro Zeile (leer = alle)';

  @override
  String get defaultProfile => 'Routing-Profil für „auto“';

  @override
  String get keySecretTitle => 'Dein neuer Router-Key';

  @override
  String get keySecretBody =>
      'Kopiere ihn jetzt – er wird nur als Hash gespeichert und kann nicht erneut angezeigt werden.';

  @override
  String get revoke => 'Widerrufen';

  @override
  String revokeConfirm(String name) {
    return '$name widerrufen? Apps, die ihn nutzen, funktionieren sofort nicht mehr.';
  }

  @override
  String get revoked => 'Widerrufen';

  @override
  String spentOf(String spent, String budget) {
    return '$spent von $budget';
  }

  @override
  String spentNoBudget(String spent) {
    return '$spent ausgegeben';
  }

  @override
  String resetsAt(String time) {
    return 'zurückgesetzt $time';
  }

  @override
  String lastUsed(String time) {
    return 'Zuletzt genutzt $time';
  }

  @override
  String get noKeysTitle => 'Keine Router-Keys';

  @override
  String get noKeysMessage =>
      'Erstelle einen Key, um deinen Endpoint aufzurufen.';

  @override
  String deleteKeyConfirm(String name) {
    return '$name endgültig löschen?';
  }

  @override
  String get routingEyebrow => 'Auto-Router';

  @override
  String get routingTitle => 'Routing';

  @override
  String get routingDescription =>
      'Wie „auto“ ein Modell wählt. Der Score kombiniert Intelligenz, gemessene Geschwindigkeit, gelernte Zuverlässigkeit und Preis; fehlerhafte oder langsame Modelle bekommen Strafpunkte und Last wird verteilt.';

  @override
  String get presetBalanced => 'Ausgewogen';

  @override
  String get presetReliable => 'Am zuverlässigsten';

  @override
  String get presetFast => 'Am schnellsten';

  @override
  String get presetSmart => 'Am klügsten';

  @override
  String get presetBest => 'Am besten';

  @override
  String get presetCheap => 'Am günstigsten';

  @override
  String get presetCustom => 'Benutzerdefiniert';

  @override
  String get presetSlider => 'Profil';

  @override
  String get weights => 'Gewichtung';

  @override
  String get constraints => 'Bedingungen';

  @override
  String get customProfiles => 'Eigene Profile';

  @override
  String get profileName => 'Profilname';

  @override
  String get saveProfile => 'Als Profil speichern';

  @override
  String get updateProfile => 'Profil aktualisieren';

  @override
  String profileUsage(String id) {
    return 'Nutze es als Modell „auto:$id“ oder als Standard eines Keys.';
  }

  @override
  String get minIntelligence => 'Mindest-Intelligenz';

  @override
  String get maxPriceOut => 'Max. Output-Preis (USD / 1 Mio., leer = egal)';

  @override
  String get adaptive => 'An Komplexität der Anfrage anpassen';

  @override
  String get adaptiveHint =>
      'Lange, code-lastige oder Tool-Anfragen verschieben Gewicht Richtung Intelligenz.';

  @override
  String get exploration => 'Lastverteilung';

  @override
  String get explorationHint =>
      'Höhere Werte verteilen Requests auf mehr gute Modelle.';

  @override
  String get excludeProviders => 'Provider ausschließen (kommagetrennt)';

  @override
  String get previewTitle => 'Live-Vorschau';

  @override
  String get previewHint =>
      'Was „auto“ gerade wählen würde, ohne einen Request zu senden.';

  @override
  String get previewPrompt => 'Test-Prompt';

  @override
  String get previewFreeOnly => 'Nur gratis Modelle';

  @override
  String get previewTools => 'Braucht Tools';

  @override
  String get previewVision => 'Braucht Vision';

  @override
  String get runPreview => 'Vorschau';

  @override
  String get score => 'Score';

  @override
  String excluded(String reason) {
    return 'Ausgeschlossen: $reason';
  }

  @override
  String get reasonNotFree => 'nicht gratis';

  @override
  String get reasonNoTools => 'keine Tools';

  @override
  String get reasonNoVision => 'kein Vision';

  @override
  String get reasonContext => 'Kontext zu klein';

  @override
  String get reasonCooldown => 'im Cooldown';

  @override
  String get reasonCircuit => 'nach Fehlern pausiert';

  @override
  String get reasonRateLimited => 'Rate-Limit';

  @override
  String get reasonQuota => 'Kontingent aufgebraucht';

  @override
  String get reasonBudget => 'über Budget';

  @override
  String get reasonPriceUnknown => 'Preis unbekannt';

  @override
  String get reasonDisabled => 'deaktiviert';

  @override
  String get reasonAccount => 'Provider nicht verfügbar';

  @override
  String get reasonAuth => 'Key abgelehnt';

  @override
  String get reasonNotAvailable => 'nicht verfügbar';

  @override
  String get reasonPayment => 'Zahlung erforderlich';

  @override
  String get reasonNoFreeQuota => 'kein Gratis-Kontingent auf diesem Account';

  @override
  String get quotasEyebrow => 'Limits';

  @override
  String get quotasTitle => 'Kontingente';

  @override
  String get quotasDescription =>
      'Verbrauch gegenüber den Limits jedes Providers. Limits stammen aus Provider-Headern, dokumentierten Free-Tiers oder werden aus Fehlern gelernt.';

  @override
  String get sharedLimits => 'Gemeinsame Account-Limits';

  @override
  String get perModelLimits => 'Pro Modell';

  @override
  String get metricRpm => 'Requests / Min.';

  @override
  String get metricRpd => 'Requests / Tag';

  @override
  String get metricTpm => 'Tokens / Min.';

  @override
  String get metricTpd => 'Tokens / Tag';

  @override
  String usedOf(String used, String limit) {
    return '$used von $limit';
  }

  @override
  String get sourceHeader => 'vom Provider';

  @override
  String get sourceCurated => 'dokumentiert';

  @override
  String get sourceLearned => 'gelernt';

  @override
  String get sourceManual => 'manuell';

  @override
  String get sourceError => 'beobachtet';

  @override
  String get balance => 'Guthaben';

  @override
  String get noQuotaData =>
      'Noch keine Limits bekannt. Sie erscheinen nach den ersten Requests.';

  @override
  String onlyFreeModels(String count) {
    return 'Gratis-Modelle ohne bekannte Limits: $count';
  }

  @override
  String get logsEyebrow => 'Einblicke';

  @override
  String get logsTitle => 'Request-Log';

  @override
  String get logsDescription =>
      'Nur Metadaten – Prompts und Antworten werden nie gespeichert.';

  @override
  String get filterErrors => 'Fehler';

  @override
  String get filterSuccess => 'Erfolgreich';

  @override
  String get fallbackTrail => 'Fallback-Verlauf';

  @override
  String attempts(String count) {
    return '$count Versuche';
  }

  @override
  String tokensInOut(String input, String output) {
    return '$input → $output Tokens';
  }

  @override
  String ttft(String value) {
    return 'TTFT $value';
  }

  @override
  String get noLogsTitle => 'Noch keine Requests';

  @override
  String get noLogsMessage =>
      'Requests erscheinen hier, sobald deine Apps den Endpoint aufrufen.';

  @override
  String get playgroundEyebrow => 'Test';

  @override
  String get playgroundTitle => 'Playground';

  @override
  String get playgroundDescription =>
      'Chatte über den echten Router mit einem deiner Keys.';

  @override
  String get selectKey => 'Router-Key';

  @override
  String get modelField => 'Modell (auto, auto:fast, provider/modell)';

  @override
  String get systemPrompt => 'System-Prompt (optional)';

  @override
  String get messageHint => 'Nachricht schreiben…';

  @override
  String get send => 'Senden';

  @override
  String get clearChat => 'Leeren';

  @override
  String routedTo(String model, String latency) {
    return 'über $model · $latency';
  }

  @override
  String get playgroundNeedsKey => 'Erstelle zuerst einen Router-Key.';

  @override
  String get settingsTitle => 'Einstellungen';

  @override
  String get settingsScopeYou => 'Du';

  @override
  String get settingsScopeServer => 'Dieser Server';

  @override
  String get settingsAccountDescription =>
      'Dein Name, deine E-Mail und dein Passwort.';

  @override
  String get settingsAppearanceDescription =>
      'Design und Sprache dieses Dashboards.';

  @override
  String get settingsConnectDescription =>
      'Fertige Einrichtung für deine SDKs und Tools.';

  @override
  String get settingsServerDescription =>
      'Registrierung, Personen und Modell-Metadaten. Nur Admins sehen diese Seite.';

  @override
  String get settingsAppliesInstantly => 'Änderungen gelten sofort';

  @override
  String get serverTitle => 'Server';

  @override
  String get profileTitle => 'Profil';

  @override
  String get themeLabel => 'Design';

  @override
  String get themeHint => 'Folgt deinem Gerät, solange du nichts auswählst.';

  @override
  String get languageHint =>
      'Nur das Dashboard. Das Gateway ist sprachneutral.';

  @override
  String get passwordHint =>
      'Mindestens 12 Zeichen. Danach wirst du abgemeldet.';

  @override
  String get signOutHint => 'In diesem Browser von NeoRouter abmelden.';

  @override
  String get appearance => 'Darstellung';

  @override
  String get themeSystem => 'System';

  @override
  String get themeLight => 'Hell';

  @override
  String get themeDark => 'Dunkel';

  @override
  String get language => 'Sprache';

  @override
  String get languageSystem => 'System';

  @override
  String get account => 'Konto';

  @override
  String get displayName => 'Anzeigename';

  @override
  String get saveProfileButton => 'Profil speichern';

  @override
  String get changePassword => 'Passwort ändern';

  @override
  String get securityTitle => 'Sicherheit';

  @override
  String get twoFactorTitle => 'Zwei-Faktor-Authentifizierung';

  @override
  String get twoFactorOn => '2FA aktiv';

  @override
  String get twoFactorOff => '2FA aus';

  @override
  String get twoFactorDisabledHint =>
      'Bei jeder Anmeldung zusätzlich einen Code aus einer Authenticator-App abfragen.';

  @override
  String twoFactorEnabledHint(String count) {
    return 'Die Anmeldung fragt nach einem Authenticator-Code. Noch $count Wiederherstellungscodes.';
  }

  @override
  String get twoFactorEnable => 'Aktivieren';

  @override
  String get twoFactorDisable => 'Deaktivieren';

  @override
  String get twoFactorNewCodes => 'Neue Wiederherstellungscodes';

  @override
  String get twoFactorPasswordPrompt =>
      'Bestätige dein Passwort, um die Zwei-Faktor-Authentifizierung einzurichten.';

  @override
  String get twoFactorContinue => 'Weiter';

  @override
  String get twoFactorScanHint =>
      'Scanne den QR-Code mit einer Authenticator-App (1Password, Google Authenticator, Authy, …) oder gib den Schlüssel von Hand ein. Danach den 6-stelligen Code eintragen.';

  @override
  String get twoFactorManualKey => 'Einrichtungsschlüssel';

  @override
  String get twoFactorCode => 'Authenticator-Code';

  @override
  String get twoFactorCodeHint =>
      '6-stelliger Code oder ein Wiederherstellungscode';

  @override
  String get twoFactorCodeOrRecovery =>
      'Authenticator- oder Wiederherstellungscode';

  @override
  String get twoFactorVerify => 'Bestätigen';

  @override
  String get twoFactorCodesHint =>
      'Bewahre diese Wiederherstellungscodes sicher auf. Jeder funktioniert einmal, falls du deinen Authenticator verlierst. Sie werden nur jetzt angezeigt.';

  @override
  String get twoFactorCodesSaved => 'Gespeichert';

  @override
  String get twoFactorDisablePrompt =>
      'Gib dein Passwort und einen aktuellen Code ein, um die Zwei-Faktor-Authentifizierung abzuschalten.';

  @override
  String get twoFactorNewCodesPrompt =>
      'Neue Codes ersetzen alle alten. Gib dein Passwort und einen aktuellen Code ein.';

  @override
  String get twoFactorDisabledNotice =>
      'Zwei-Faktor-Authentifizierung ist aus.';

  @override
  String get twoFactorBack => 'Anderes Konto verwenden';

  @override
  String get twoFactorLoginTitle => 'Anmeldung bestätigen';

  @override
  String get twoFactorLoginSubtitle =>
      'Gib den Code aus deiner Authenticator-App ein.';

  @override
  String get currentPassword => 'Aktuelles Passwort';

  @override
  String get newPassword => 'Neues Passwort';

  @override
  String get passwordChanged => 'Passwort geändert. Bitte erneut anmelden.';

  @override
  String get connectApps => 'Apps verbinden';

  @override
  String get connectAppsHint => 'Ersetze nrt_… durch einen deiner Router-Keys.';

  @override
  String get registrationOpenLabel => 'Neue Registrierungen erlauben';

  @override
  String get registrationOpenHint =>
      'Wenn aus, können sich nur bestehende Konten anmelden.';

  @override
  String get users => 'Benutzer';

  @override
  String get roleAdmin => 'Admin';

  @override
  String get roleUser => 'Benutzer';

  @override
  String get you => 'du';

  @override
  String get disableUser => 'Deaktivieren';

  @override
  String get enableUser => 'Aktivieren';

  @override
  String get makeAdmin => 'Zum Admin machen';

  @override
  String get makeUser => 'Zum Benutzer machen';

  @override
  String deleteUserConfirm(String name) {
    return '$name mit allen Providern, Keys und Logs löschen?';
  }

  @override
  String get metadataSources => 'Modell-Metadaten';

  @override
  String get metadataHint =>
      'Preise, Kontextgrößen und Intelligenz-Schätzungen kommen von models.dev und OpenRouter. Ein Artificial-Analysis-Key ergänzt benchmark-basierte Intelligenz-Scores.';

  @override
  String get aaKey => 'Artificial-Analysis-API-Key (optional)';

  @override
  String get aaConfigured => 'Artificial-Analysis-Key ist hinterlegt.';

  @override
  String get refreshMetadata => 'Metadaten jetzt aktualisieren';

  @override
  String get metadataRefreshed => 'Metadaten aktualisiert.';

  @override
  String lastFetched(String source, String time) {
    return '$source: $time';
  }

  @override
  String get notFetched => 'noch nicht geladen';

  @override
  String get navAnalytics => 'Analytics';

  @override
  String get analyticsEyebrow => 'Einblicke';

  @override
  String get analyticsTitle => 'Analytics';

  @override
  String get analyticsDescription =>
      'Wie sich Traffic, Latenz, Kosten und Provider über die Zeit verhalten.';

  @override
  String get chartRequests => 'Requests';

  @override
  String get chartSuccess => 'Erfolgreich';

  @override
  String get chartErrors => 'Fehler';

  @override
  String get chartLatency => 'Latenz';

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
  String get chartCost => 'Kosten (USD)';

  @override
  String get chartFreeShare => 'Gratis bedient';

  @override
  String get providerShare => 'Traffic nach Provider';

  @override
  String get modelTable => 'Modelle';

  @override
  String get keyUsage => 'Router-Keys';

  @override
  String get apiFormats => 'API-Formate';

  @override
  String get attemptOutcomes => 'Ergebnisse der Versuche';

  @override
  String get attemptOutcomesHint =>
      'Jeder Upstream-Versuch, inklusive Fallbacks und Hedges.';

  @override
  String get latencyDistribution => 'Latenz-Verteilung';

  @override
  String get colRequests => 'Requests';

  @override
  String get colSuccess => 'Erfolg';

  @override
  String get colLatency => 'p50 / p95';

  @override
  String get colTokens => 'Tokens';

  @override
  String get colCost => 'Kosten';

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
      'Es werden die letzten 100.000 Requests dieses Zeitraums gezeigt.';

  @override
  String get fetchModels => 'Modelle abrufen';

  @override
  String modelsFound(String count) {
    return '$count Modelle gefunden';
  }

  @override
  String modelsFetchFailed(String message) {
    return 'Modelle konnten nicht abgerufen werden: $message';
  }

  @override
  String get manualModels => 'Zusätzliche Modell-IDs (eine pro Zeile)';

  @override
  String get manualModelsHint =>
      'Für Endpoints ohne /models-Liste oder mit fehlenden Modellen.';

  @override
  String get authHeaderLabel => 'Key-Header (leer = Authorization: Bearer)';

  @override
  String get customHeaders =>
      'Zusätzliche Header (Name: Wert, einer pro Zeile)';

  @override
  String get modelsPathLabel => 'Modell-Pfad (leer = /models)';

  @override
  String get advancedOptions => 'Erweitert';

  @override
  String keyStatus(String status) {
    return 'Key: $status';
  }

  @override
  String get connectOpenRouter => 'Mit OpenRouter verbinden';

  @override
  String get connectOpenRouterHint =>
      'Ein Klick: bei OpenRouter anmelden, der Key wird automatisch erstellt und gespeichert.';

  @override
  String get orManualKey => 'oder Key einfügen';

  @override
  String get openRouterConnected =>
      'OpenRouter verbunden – Modelle importiert.';

  @override
  String openRouterFailed(String message) {
    return 'OpenRouter-Verbindung fehlgeschlagen: $message';
  }

  @override
  String get navAdmin => 'Admin';

  @override
  String get adminDescription =>
      'Diesen Server betreiben: Konten, Registrierung, Server-Details und Modell-Metadaten.';

  @override
  String get adminSearchHint =>
      'Admin-Einstellungen suchen, z. B. Registrierung';

  @override
  String get nothingMatches => 'Keine Treffer';

  @override
  String get registrationTitle => 'Registrierung';

  @override
  String usersCount(String count) {
    return '$count Konten auf diesem Server.';
  }

  @override
  String get serverInfoHint =>
      'Was diese NeoRouter-Instanz ausführt und wo Apps sie erreichen.';

  @override
  String get versionLabel => 'Version';

  @override
  String get capabilitiesLabel => 'Fähigkeiten';

  @override
  String get settingsSearchHint => 'Einstellungen suchen';

  @override
  String get settingsScopeApp => 'Diese App';

  @override
  String get settingsScopeAppHint => 'Gilt für diesen Browser';

  @override
  String get settingsScopeYouHint => 'In deinem Konto gespeichert';

  @override
  String get settingsSavedOnSubmit => 'Wird beim Speichern übernommen';
}
