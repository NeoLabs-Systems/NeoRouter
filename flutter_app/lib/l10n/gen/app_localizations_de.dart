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
  String get navProviders => 'Verbindungen';

  @override
  String get navModels => 'Modelle';

  @override
  String get navKeys => 'API-Keys';

  @override
  String get navRouting => 'Smart Routing';

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
  String get noTrafficTitle => 'Noch nichts zu sehen';

  @override
  String get noTrafficMessage =>
      'Sobald deine Apps Anfragen senden, siehst du hier wie viele, wie schnell und welche Modelle geantwortet haben.';

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
      'Anfragen überspringen sie, bis das behoben ist.';

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
      'Noch keine Anfragen. Sende deine erste Nachricht im Playground.';

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
  String get providersTitle => 'Verbindungen';

  @override
  String get providersDescription =>
      'Die KI-Anbieter, die NeoRouter nutzen darf. Keys werden beim Hinzufügen geprüft und verschlüsselt gespeichert.';

  @override
  String get addProvider => 'Verbindung hinzufügen';

  @override
  String get chooseProvider => 'Verbindung hinzufügen';

  @override
  String get searchProviders => 'Provider suchen…';

  @override
  String get categoryFree => 'Gratis nutzbar';

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
  String get categorySearch => 'Websuche';

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
      'Wähle den Gratis-Tarif nur, wenn bei diesem Anbieter keine Zahlungsmethode hinterlegt ist. NeoRouter prüft das, wo möglich.';

  @override
  String get billingAlwaysFreeNote =>
      'Einige Modelle dieses Providers sind unabhängig von der Abrechnung immer gratis.';

  @override
  String get getKey => 'Key holen';

  @override
  String get checkAndAdd => 'Verbinden';

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
  String get statusError => 'Nicht erreichbar';

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
  String get removeProvider => 'Verbindung entfernen';

  @override
  String removeProviderConfirm(String name) {
    return '$name entfernen? Seine Modelle werden nicht mehr genutzt. Du kannst die Verbindung jederzeit neu hinzufügen.';
  }

  @override
  String get editProvider => 'Bearbeiten';

  @override
  String lastChecked(String time) {
    return 'Geprüft $time';
  }

  @override
  String syncedModels(String count) {
    return '$count Modelle synchronisiert.';
  }

  @override
  String get noProvidersTitle => 'Noch keine Verbindungen';

  @override
  String get noProvidersMessage =>
      'Verbinde einen KI-Anbieter und NeoRouter importiert alle seine Modelle.';

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
      'Alles, was deine Verbindungen anbieten. NeoRouter lernt, wie klug, schnell und zuverlässig jedes Modell ist, und wählt das beste für dich.';

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
  String get intelligence => 'Klug';

  @override
  String get speed => 'Schnell';

  @override
  String get reliability => 'Zuverlässig';

  @override
  String get cost => 'Günstig';

  @override
  String get price => 'Preis';

  @override
  String pricePerMillion(String input, String output) {
    return '$input / $output pro 1 Mio. Tokens';
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
  String get overrideFree => 'Gratis oder bezahlt';

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
  String get enableAll => 'Alle einschalten';

  @override
  String get disableAll => 'Alle ausschalten';

  @override
  String get enableFreeOnly => 'Nur Gratis-Modelle nutzen';

  @override
  String measured(String latency, String tps, String samples) {
    return '$latency · $tps Tokens/s · $samples Anfragen';
  }

  @override
  String get notMeasured => 'Noch nicht genutzt';

  @override
  String penaltyLabel(String value) {
    return 'Strafpunkte $value';
  }

  @override
  String unavailableUntil(String reason, String time) {
    return '$reason bis $time';
  }

  @override
  String get noModelsTitle => 'Keine Modelle gefunden';

  @override
  String get noModelsMessage =>
      'Probiere einen anderen Filter oder verbinde einen weiteren Anbieter.';

  @override
  String modelsShown(String count) {
    return '$count Modelle';
  }

  @override
  String get keysEyebrow => 'Zugriff';

  @override
  String get keysTitle => 'API-Keys';

  @override
  String get keysDescription =>
      'Mit Keys nutzen deine Apps NeoRouter. Lege pro Key fest, ob er bezahlte Modelle nutzen darf und wie viel er ausgeben darf.';

  @override
  String get createKey => 'Key erstellen';

  @override
  String get editKey => 'Key bearbeiten';

  @override
  String get keyName => 'Name';

  @override
  String get keyMode => 'Modellzugriff';

  @override
  String get modeFreeOnly => 'Nur Gratis-Modelle';

  @override
  String get modePaid => 'Alle Modelle';

  @override
  String get modeBoth => 'Alle Modelle, dann gratis';

  @override
  String get budget => 'Limit in USD (leer = kein Limit)';

  @override
  String get budgetPeriod => 'Budget-Zeitraum';

  @override
  String get periodTotal => 'Insgesamt';

  @override
  String get periodMonthly => 'Monatlich';

  @override
  String get periodDaily => 'Täglich';

  @override
  String get rpmLimit => 'Requests pro Minute (leer = unbegrenzt)';

  @override
  String get allowedModels =>
      'Erlaubte Modelle – Glob-Muster, eines pro Zeile (leer = alle)';

  @override
  String get defaultProfile => 'Routing-Stil für „auto“';

  @override
  String get keySecretTitle => 'Kopiere deinen neuen Key';

  @override
  String get keySecretBody =>
      'Aus Sicherheitsgründen wird er nur einmal angezeigt. Füge ihn in deiner App dort ein, wo nach einem API-Key gefragt wird.';

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
    return '$spent von $budget ausgegeben';
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
  String get noKeysTitle => 'Noch keine API-Keys';

  @override
  String get noKeysMessage =>
      'Erstelle einen Key und füge ihn in jede App ein, die mit OpenAI oder Claude funktioniert.';

  @override
  String deleteKeyConfirm(String name) {
    return '$name endgültig löschen?';
  }

  @override
  String get routingEyebrow => 'Auto-Router';

  @override
  String get routingTitle => 'Smart Routing';

  @override
  String get routingDescription =>
      'Fragt eine App nach „auto“, wählt NeoRouter für jede Anfrage das Modell. Bestimme, was dir am wichtigsten ist.';

  @override
  String get presetBalanced => 'Ausgewogen';

  @override
  String get presetReliable => 'Am zuverlässigsten';

  @override
  String get presetFast => 'Am schnellsten';

  @override
  String get presetSmart => 'Am klügsten';

  @override
  String get presetBest => 'Insgesamt am besten';

  @override
  String get presetCheap => 'Am günstigsten';

  @override
  String get presetCustom => 'Mein Stil';

  @override
  String get presetSlider => 'Profil';

  @override
  String get weights => 'Gewichtung';

  @override
  String get constraints => 'Bedingungen';

  @override
  String get customProfiles => 'Eigene Profile';

  @override
  String get profileName => 'Name';

  @override
  String get saveProfile => 'Als Profil speichern';

  @override
  String get updateProfile => 'Profil aktualisieren';

  @override
  String profileUsage(String id) {
    return 'Nutze es als Modell „auto:$id“ oder als Standard eines Keys.';
  }

  @override
  String get minIntelligence => 'Mindestqualität';

  @override
  String get maxPriceOut =>
      'Max. Preis pro 1 Mio. Ausgabe-Tokens (USD, leer = egal)';

  @override
  String get adaptive => 'An die Anfrage anpassen';

  @override
  String get adaptiveHint =>
      'Lange oder code-lastige Anfragen bevorzugen klügere Modelle.';

  @override
  String get exploration => 'Last verteilen';

  @override
  String get explorationHint =>
      'Höhere Werte verteilen Anfragen auf mehr gute Modelle statt immer das beste zu nehmen.';

  @override
  String get excludeProviders =>
      'Diese Verbindungen nie nutzen (mit Komma getrennt)';

  @override
  String get previewTitle => 'Gerade würde es wählen';

  @override
  String previewHint(String style) {
    return 'Welche Modelle „$style“ gerade wählen würde – es wird nichts gesendet.';
  }

  @override
  String get previewPrompt => 'Test-Prompt';

  @override
  String get previewFreeOnly => 'Nur Gratis-Modelle';

  @override
  String get previewTools => 'Braucht Tools';

  @override
  String get previewVision => 'Braucht Vision';

  @override
  String get runPreview => 'Prüfen';

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
  String get metricRpm => 'Anfragen pro Minute';

  @override
  String get metricRpd => 'Anfragen pro Tag';

  @override
  String get metricTpm => 'Tokens pro Minute';

  @override
  String get metricTpd => 'Tokens pro Tag';

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
      'Noch keine Limits bekannt. Sie erscheinen nach den ersten Anfragen.';

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
  String get selectKey => 'API-Key';

  @override
  String get modelField => 'Modell (auto, auto:fast, provider/modell)';

  @override
  String get systemPrompt => 'System-Prompt (optional)';

  @override
  String get messageHint => 'Nachricht an NeoRouter…';

  @override
  String get send => 'Senden';

  @override
  String get clearChat => 'Leeren';

  @override
  String routedTo(String model, String latency) {
    return '$model · $latency';
  }

  @override
  String get playgroundNeedsKey =>
      'Erstelle zuerst einen API-Key – der Playground nutzt ihn genau wie deine Apps.';

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
  String get chartRequests => 'Anfragen';

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

  @override
  String get navHome => 'Start';

  @override
  String get navActivity => 'Aktivität';

  @override
  String get navSectionServer => 'Server';

  @override
  String get capChat => 'Chat';

  @override
  String get capEmbedding => 'Embeddings';

  @override
  String get capRerank => 'Ranking';

  @override
  String get capImage => 'Bilder';

  @override
  String get capAudio => 'Audio';

  @override
  String get capSpeech => 'Sprachausgabe';

  @override
  String get capSearch => 'Websuche';

  @override
  String get capTranscription => 'Transkription';

  @override
  String get capVideo => 'Video';

  @override
  String get capModeration => 'System-One-Modelle';

  @override
  String get capOther => 'Sonstige';

  @override
  String get capChatHint =>
      'Text- und Vision-Modelle, die auf Nachrichten antworten.';

  @override
  String get capEmbeddingHint =>
      'Wandeln Text in Vektoren für Suche und Gedächtnis um.';

  @override
  String get capRerankHint => 'Sortieren Suchergebnisse nach Relevanz.';

  @override
  String get capImageHint => 'Erstellen und bearbeiten Bilder.';

  @override
  String get capAudioHint => 'Sprache zu Text, Text zu Sprache und Musik.';

  @override
  String get capSpeechHint => 'Lesen Text mit natürlicher Stimme vor.';

  @override
  String get capSearchHint =>
      'Finden aktuelle Seiten im Web, für dich und als Werkzeug für Chat-Modelle.';

  @override
  String get capTranscriptionHint =>
      'Machen aus Aufnahmen Text und Untertitel.';

  @override
  String get capVideoHint => 'Erstellen kurze Videos aus einer Beschreibung.';

  @override
  String get capModerationHint =>
      'Schnelle Entscheidungsmodelle wie Jev: Sie klassifizieren, prüfen und entscheiden in Sekundenbruchteilen, statt lange Antworten zu schreiben.';

  @override
  String get capOtherHint =>
      'Spezialmodelle, z. B. für Übersetzung oder Dokumente.';

  @override
  String capComingSoon(String count) {
    return 'NeoRouter hat bereits $count dieser Modelle in deinen Verbindungen gefunden und wird sie in einem kommenden Update routen.';
  }

  @override
  String get capSoon => 'Bald';

  @override
  String greetMorning(String name) {
    return 'Guten Morgen, $name';
  }

  @override
  String greetAfternoon(String name) {
    return 'Guten Tag, $name';
  }

  @override
  String greetEvening(String name) {
    return 'Guten Abend, $name';
  }

  @override
  String homeStatusOk(String count) {
    return 'Alles läuft. $count Verbindungen funktionieren.';
  }

  @override
  String homeStatusIssues(String count) {
    return '$count Verbindung braucht deine Aufmerksamkeit. Anfragen laufen solange über die anderen.';
  }

  @override
  String get homeStatusEmpty => 'Verbinde einen KI-Anbieter, um loszulegen.';

  @override
  String get homeLast7Days => 'Letzte 7 Tage';

  @override
  String get metricRequests => 'Anfragen';

  @override
  String get metricAnswered => 'Beantwortet';

  @override
  String get metricSpent => 'Ausgegeben';

  @override
  String metricFreeShare(String percent) {
    return '$percent % gratis';
  }

  @override
  String get metricResponse => 'Ø Antwortzeit';

  @override
  String get homeSeeAll => 'Alle anzeigen';

  @override
  String get homeRecentActivity => 'Letzte Aktivität';

  @override
  String homeModelsLine(String total, String free) {
    return '$total Modelle · $free gratis';
  }

  @override
  String get homeUseTitle => 'NeoRouter in deinen Apps nutzen';

  @override
  String get homeUseBody =>
      'Trage diese Adresse und einen deiner API-Keys in jede App ein, die mit OpenAI oder Claude funktioniert.';

  @override
  String get homeGetKey => 'API-Key holen';

  @override
  String get homeShowExamples => 'Code-Beispiele zeigen';

  @override
  String get homeHideExamples => 'Code-Beispiele ausblenden';

  @override
  String get setupTitle => 'Einrichtung abschließen';

  @override
  String setupProgress(String done, String total) {
    return '$done von $total erledigt';
  }

  @override
  String get setupStep1Title => 'KI-Anbieter verbinden';

  @override
  String get setupStep1Body =>
      'Füge einen Key von Groq, Google, OpenRouter oder einem anderen Anbieter hinzu. NeoRouter findet alle Modelle.';

  @override
  String get setupStep2Title => 'API-Key erstellen';

  @override
  String get setupStep2Body =>
      'Ein Key für deine Apps, optional nur für Gratis-Modelle oder mit Budget.';

  @override
  String get setupStep3Title => 'Erste Nachricht senden';

  @override
  String get setupStep3Body => 'Probiere es im Playground aus.';

  @override
  String get setupStart => 'Los';

  @override
  String problemTitle(String name) {
    return '$name braucht Aufmerksamkeit';
  }

  @override
  String get problemKeyRejected =>
      'Der Anbieter hat den Key abgelehnt. Aktualisiere ihn, um die Verbindung wieder zu nutzen.';

  @override
  String requestFailed(String reason) {
    return 'Fehlgeschlagen: $reason';
  }

  @override
  String get agoNow => 'gerade eben';

  @override
  String agoMinutes(String count) {
    return 'vor $count Min.';
  }

  @override
  String agoHours(String count) {
    return 'vor $count Std.';
  }

  @override
  String get activityDescription =>
      'Was deine Apps angefragt haben und wie NeoRouter geantwortet hat. Gespeichert werden nur Metadaten – nie deine Nachrichten.';

  @override
  String get tabOverview => 'Überblick';

  @override
  String get tabRequests => 'Anfragen';

  @override
  String metricTokens(String count) {
    return '$count Tokens';
  }

  @override
  String get metricTypicalResponse => 'Typische Antwortzeit';

  @override
  String metricSlowest(String value) {
    return 'langsamste 5 %: $value';
  }

  @override
  String get chartResponseTime => 'Antwortzeit';

  @override
  String get chartTypical => 'Typisch';

  @override
  String get chartSlowest => 'Langsamste 5 %';

  @override
  String get topModelsTitle => 'Meistgenutzte Modelle';

  @override
  String get byConnection => 'Nach Verbindung';

  @override
  String get byKey => 'Nach API-Key';

  @override
  String rankingLine(String count, String latency) {
    return '$count · $latency';
  }

  @override
  String tooltipRequests(String count, String errors) {
    return '$count Anfragen · $errors fehlgeschlagen';
  }

  @override
  String get filterAnswered => 'Beantwortet';

  @override
  String get filterFailed => 'Fehlgeschlagen';

  @override
  String get logsPrivacyHint =>
      'NeoRouter speichert nie, was du oder das Modell geschrieben habt – nur welches Modell geantwortet hat, wie schnell und was es gekostet hat.';

  @override
  String get detailRequested => 'Angefragt';

  @override
  String get detailKey => 'API-Key';

  @override
  String get detailResponse => 'Antwortzeit';

  @override
  String get detailFirstToken => 'Erste Wörter nach';

  @override
  String get detailTokens => 'Tokens';

  @override
  String get detailCost => 'Kosten';

  @override
  String get detailFormat => 'Schnittstelle';

  @override
  String get detailTime => 'Zeit';

  @override
  String get detailRoute => 'Weg';

  @override
  String get outcomeAnswered => 'hat geantwortet';

  @override
  String get outcomeSlower => 'war langsamer – abgebrochen';

  @override
  String get outcomeBusy => 'war ausgelastet – übersprungen';

  @override
  String outcomeFailed(String reason) {
    return 'fehlgeschlagen ($reason)';
  }

  @override
  String get tabConnected => 'Verbunden';

  @override
  String get tabLimits => 'Nutzungslimits';

  @override
  String get accountFreePlan => 'Gratis-Tarif';

  @override
  String get accountPaidPlan => 'Bezahlkonto';

  @override
  String get accountFreeVerified => 'Gratis-Tarif, bestätigt';

  @override
  String get accountBillingActive =>
      'Abrechnung aktiv – nichts zählt als gratis';

  @override
  String get accountNotVerified => 'Gratis-Tarif, noch nicht bestätigt';

  @override
  String get moreActions => 'Weitere Aktionen';

  @override
  String get chooseProviderHint =>
      'Wähle den Dienst, für den du einen Key hast. Anbieter mit „Gratis-Modelle“ kannst du ohne Bezahlung nutzen.';

  @override
  String get providerFreeTag => 'Gratis-Modelle';

  @override
  String getKeyAt(String name) {
    return 'Key bei $name holen';
  }

  @override
  String get billingPaidHint =>
      'Anfragen an kostenpflichtige Modelle rechnet der Anbieter ab; sie zählen gegen das Budget deiner API-Keys.';

  @override
  String get testConnection => 'Verbindung testen';

  @override
  String probeOk(String count) {
    return 'Der Key funktioniert – $count Modelle gefunden.';
  }

  @override
  String get limitsDescription =>
      'Was vom Kontingent jedes Anbieters heute noch übrig ist. Minutenlimits setzen sich von selbst zurück und NeoRouter weicht ihnen aus, daher siehst du nur Tageskontingente und Modelle, die knapp werden.';

  @override
  String limitResets(String time) {
    return 'zurückgesetzt $time';
  }

  @override
  String get limitConcurrent => 'Gleichzeitige Anfragen';

  @override
  String limitConcurrentValue(String count) {
    return 'bis $count, aus den Antworten gelernt';
  }

  @override
  String limitConcurrentDocumented(String count) {
    return 'bis $count';
  }

  @override
  String get addAnotherAccount => 'Weiteres Konto hinzufügen';

  @override
  String limitsModelsRoom(String room, String total) {
    return '$room von $total Modellen haben heute noch Kontingent';
  }

  @override
  String get filterProvider => 'Nach Verbindung filtern';

  @override
  String get allConnections => 'Alle Verbindungen';

  @override
  String showMore(String count) {
    return 'Mehr anzeigen (noch $count)';
  }

  @override
  String get factWhyFree => 'Gratis oder bezahlt';

  @override
  String get factContext => 'Kontext';

  @override
  String get factMeasured => 'Gemessen';

  @override
  String get modeFreeOnlyHint => 'Kostet nie etwas. Empfohlen.';

  @override
  String get modeBothHint =>
      'Nutzt bezahlte Modelle, bis das Budget aufgebraucht ist, und macht dann mit Gratis-Modellen weiter.';

  @override
  String get modePaidHint =>
      'Nutzt das beste Modell unabhängig vom Preis und stoppt beim Budget.';

  @override
  String get keyAccessTitle => 'Was darf dieser Key nutzen?';

  @override
  String get keyBudgetTitle => 'Ausgabenlimit';

  @override
  String get keyNameHint => 'z. B. Laptop oder Home Assistant';

  @override
  String get keySecretDone => 'Fertig';

  @override
  String keyUsed(String time) {
    return 'genutzt $time';
  }

  @override
  String get keyNeverUsed => 'noch nicht genutzt';

  @override
  String revokedKeys(String count) {
    return 'Widerrufene Keys ($count)';
  }

  @override
  String get keysFooter =>
      'Gib jeder App einen eigenen Key. Gerät einer in falsche Hände, widerrufe ihn – deine anderen Apps laufen weiter.';

  @override
  String get presetBalancedHint =>
      'Eine gute Mischung aus Qualität, Tempo und Kosten. Standard, wenn nichts anderes eingestellt ist.';

  @override
  String get presetFastHint => 'Die kürzeste Wartezeit auf eine Antwort.';

  @override
  String get presetSmartHint =>
      'Die fähigsten Modelle, auch wenn sie länger brauchen.';

  @override
  String get presetReliableHint => 'Modelle, die fast nie ausfallen.';

  @override
  String get presetBestHint =>
      'Hohe Qualität von Modellen, die auch zuverlässig laufen.';

  @override
  String get presetCheapHint =>
      'Die geringsten Kosten – Gratis-Modelle zuerst.';

  @override
  String get stylesTitle => 'Routing-Stile';

  @override
  String get stylesHint =>
      'Nutze einen Stil über seinen Modellnamen, z. B. „auto:fast“, oder wähle ihn pro API-Key.';

  @override
  String get customStylesTitle => 'Eigene Stile';

  @override
  String get customStylesHint =>
      'Lege selbst fest, wie wichtig Qualität, Tempo, Zuverlässigkeit und Kosten sind.';

  @override
  String get createStyle => 'Neuer Stil';

  @override
  String get editStyle => 'Stil bearbeiten';

  @override
  String get weightsTitle => 'Was zählt';

  @override
  String get weightsHint => 'Je weiter rechts, desto wichtiger.';

  @override
  String get previewDefaultPrompt => 'Erkläre Quicksort in zwei Sätzen.';

  @override
  String get previewNone =>
      'Gerade passt kein Modell. Verbinde einen Anbieter oder schalte „Nur Gratis-Modelle“ aus.';

  @override
  String previewSkipped(String count) {
    return '$count Modelle übersprungen';
  }

  @override
  String get previewMostLikely => 'am wahrscheinlichsten';

  @override
  String get playgroundEmptyTitle => 'Was möchtest du ausprobieren?';

  @override
  String get playgroundEmptyHint =>
      'Nachrichten laufen mit deinem Key über den echten Router – du siehst, welches Modell antwortet und wie schnell.';

  @override
  String get suggestion1 =>
      'Erkläre in zwei Sätzen, wie ein Regenbogen entsteht';

  @override
  String get suggestion2 =>
      'Schreibe eine freundliche Erinnerung an ein Team-Meeting';

  @override
  String get suggestion3 =>
      'Gib mir drei Ideen fürs Abendessen mit Kichererbsen';

  @override
  String get newChat => 'Neuer Chat';

  @override
  String get modelPicker => 'Modell';

  @override
  String get modelSpecific => 'Bestimmtes Modell…';

  @override
  String get modelSpecificHint => 'z. B. groq/openai/gpt-oss-120b';

  @override
  String get systemPromptTitle => 'Anweisungen';

  @override
  String get systemPromptHint =>
      'Optional: wie sich der Assistent verhalten soll, z. B. „Antworte kurz und auf Deutsch.“';

  @override
  String get thinking => 'Denkt nach…';

  @override
  String get enterToSend => 'Enter zum Senden · Shift+Enter für neue Zeile';

  @override
  String get groupModels => 'Modelle';

  @override
  String get groupAccess => 'Zugang';

  @override
  String capComingSoonTitle(String kind) {
    return '$kind – kommt bald';
  }

  @override
  String get capRouter => 'Andere Router';

  @override
  String get capRouterHint =>
      'Dienste wie OpenRouter Auto oder Jev Router, die selbst ein Modell wählen. NeoRouter übernimmt das Routing für dich, daher werden sie nur aufgelistet.';

  @override
  String get metricSaved => 'Gespart mit Gratis-Modellen';

  @override
  String metricSavedCaption(String spent, String percent) {
    return '$spent ausgegeben · $percent % gratis';
  }

  @override
  String get styleBuiltIn => 'Fertige Stile';

  @override
  String get styleUseTitle => 'Diesen Stil nutzen';

  @override
  String get styleUseHint =>
      'Frage in jeder App nach diesem Modellnamen oder wähle den Stil für einen API-Key.';

  @override
  String get styleUseRow => 'Modellname für deine Apps';

  @override
  String get styleWeightsHint =>
      'Wie stark jede Eigenschaft beim Vergleich der Modelle zählt. Ein Modell muss zudem in allem, was zählt, ordentlich sein.';

  @override
  String get freeMode => 'Nur gratis';

  @override
  String get freeModeOnHint =>
      'Nur Gratis-Modelle antworten. Ausschalten, um auch bezahlte Modelle zu erlauben, die dieser Key nutzen darf.';

  @override
  String get freeModeOffHint =>
      'Auch bezahlte Modelle dürfen antworten, im Rahmen des Budgets dieses Keys.';

  @override
  String get freeModeLocked => 'Dieser API-Key nutzt nur Gratis-Modelle.';

  @override
  String get freeModeLockedExplain =>
      'Dieser API-Key ist auf „Nur Gratis-Modelle“ gestellt und bleibt daher immer gratis. Für bezahlte Modelle wähle einen anderen Key oder ändere diesen unter API-Keys.';

  @override
  String get navProcessing => 'Datenschutz & Kompression';

  @override
  String get processingTitle => 'Datenschutz & Kompression';

  @override
  String get processingDescription =>
      'Was mit jeder Anfrage passiert, bevor sie deinen Server verlässt: private Daten vor Anbietern verbergen und Prompts verkleinern.';

  @override
  String get privacyShield => 'Datenschutzschild';

  @override
  String get compressionTitle => 'Kompression';

  @override
  String get privacyHiddenValues => 'Verborgene Werte';

  @override
  String get privacyProtectedRequests => 'Geschützte Anfragen';

  @override
  String get last30Days => 'letzte 30 Tage';

  @override
  String ofRequests(String count) {
    return 'von $count Anfragen';
  }

  @override
  String get privacyHowItWorks =>
      'Private Werte werden durch Platzhalter wie [EMAIL_1] ersetzt, bevor eine Anfrage deinen Server verlässt. Die KI sieht nur den Platzhalter, NeoRouter setzt den echten Wert in die Antwort wieder ein.';

  @override
  String get privacyEnable => 'Private Daten verbergen';

  @override
  String get privacyEnableHint => 'Gilt für alle API-Keys und den Playground.';

  @override
  String get privacyWhatToHide => 'Was verborgen wird';

  @override
  String get privacyModesHint =>
      '„Verbergen & zurück“ setzt den Wert in der Antwort wieder ein. „Nur verbergen“ lässt ihn dauerhaft verborgen – ideal für Passwörter und Kartennummern.';

  @override
  String get modeOff => 'Aus';

  @override
  String get modeRestore => 'Verbergen & zurück';

  @override
  String get modeHide => 'Nur verbergen';

  @override
  String get catEmail => 'E-Mail-Adressen';

  @override
  String get catEmailHint => 'anna@example.com';

  @override
  String get catPhone => 'Telefonnummern';

  @override
  String get catPhoneHint => 'Internationale und lokale Formate.';

  @override
  String get catIban => 'Bankkonten (IBAN)';

  @override
  String get catIbanHint =>
      'Mit der IBAN-Prüfsumme geprüft, zufällige Zahlen bleiben unberührt.';

  @override
  String get catCard => 'Zahlungskarten';

  @override
  String get catCardHint =>
      'Nur echte Kartennummern (Herausgeber und Prüfsumme passen).';

  @override
  String get catIp => 'IP-Adressen';

  @override
  String get catIpHint => 'Außer localhost.';

  @override
  String get catSecret => 'Passwörter & API-Keys';

  @override
  String get catSecretHint =>
      'Bekannte Key-Formate, „password: …“, Passwörter in Links und zufällig aussehende Tokens.';

  @override
  String get catCustom => 'Deine privaten Begriffe';

  @override
  String get catCustomHint => 'Die Begriffe, die du unten einträgst.';

  @override
  String get catPerson => 'Namen von Personen';

  @override
  String get catPersonHint => 'Per KI-Erkennung gefunden.';

  @override
  String get catAddress => 'Adressen';

  @override
  String get catAddressHint => 'Per KI-Erkennung gefunden.';

  @override
  String get catOrganization => 'Organisationen';

  @override
  String get catOrganizationHint =>
      'Arbeitgeber, Praxen, Schulen – per KI-Erkennung gefunden.';

  @override
  String get catId => 'Ausweis- und Kundennummern';

  @override
  String get catIdHint =>
      'Kunden-, Vertrags-, Versicherungs- und Steuernummern – per KI-Erkennung gefunden.';

  @override
  String get privacyTermsTitle => 'Deine privaten Begriffe';

  @override
  String get privacyTermsHint =>
      'Namen, deine Firma, deine Straße – alles, was nie bei einem Anbieter ankommen soll. Ein Begriff pro Zeile.';

  @override
  String get privacyTermsPlaceholder =>
      'Anna Schmidt\nAcme Holding\nHauptstraße 5';

  @override
  String get privacyAiTitle => 'KI-Erkennung';

  @override
  String get privacyAiHint =>
      'Muster erkennen keine Namen oder Adressen. Ein schnelles KI-Modell schon: Es liest jede neue Nachricht und markiert private Angaben, die dann ebenso verborgen werden.';

  @override
  String get privacyAiEnable => 'Namen, Adressen und Nummern per KI finden';

  @override
  String get privacyAiEnableHint =>
      'Kostet bei neuen Nachrichten etwa eine Sekunde; wiederholte Nachrichten werden gemerkt.';

  @override
  String get privacyAiModel => 'Erkennungsmodell';

  @override
  String get privacyAiModelHint =>
      '„auto:fast“ wählt das schnellste Modell. Nimm ein lokales Modell, z. B. „ollama/…“, wenn kein Anbieter den Rohtext sehen soll.';

  @override
  String get privacyAiFreeOnly => 'Für die Erkennung nur Gratis-Modelle nutzen';

  @override
  String get privacyAiWarning =>
      'Das Erkennungsmodell sieht den Originaltext neuer Nachrichten. Wähle einen Anbieter, dem du vertraust, oder ein lokales Modell.';

  @override
  String get privacyTryHint =>
      'Füge einen Text ein und sieh genau, was ein Anbieter bekäme. Es wird nichts an ein Modell gesendet (außer der KI-Erkennung, falls aktiv).';

  @override
  String get privacyTrySample =>
      'Hallo, ich bin Anna Schmidt. Bitte schick die Rechnung an anna.schmidt@example.de und ruf mich unter +49 170 1234567 an. Meine IBAN ist DE89 3704 0044 0532 0130 00, das Server-Passwort: hunter2pw.';

  @override
  String privacyTryResult(String count) {
    return '$count Werte verborgen';
  }

  @override
  String get privacyFootnote =>
      'Verborgene Werte verlassen nie deinen Server und werden nie gespeichert; die Zuordnung existiert nur während einer Anfrage. Die Erkennung ist ein starkes Sicherheitsnetz, aber keine Garantie – halte sehr sensible Daten aus Prompts heraus.';

  @override
  String get compressionSaved => 'Gesparte Tokens';

  @override
  String get compressionRequests => 'Komprimierte Anfragen';

  @override
  String get compressionLevel => 'Stufe';

  @override
  String get compressionLevelHint =>
      'Kleinere Prompts sind günstiger, schneller und passen in mehr Modelle. Höhere Stufen sparen mehr, lassen aber ältere Details weg.';

  @override
  String get levelOff => 'Aus';

  @override
  String get levelOffHint =>
      'Anfragen gehen genau so raus, wie deine App sie geschrieben hat.';

  @override
  String get levelLossless => 'Verlustfrei';

  @override
  String get levelLosslessHint =>
      'Entfernt überflüssige Leerzeichen, verkleinert JSON und streicht doppelte Blöcke. Der Inhalt bleibt gleich, Code wird nicht angefasst.';

  @override
  String get levelBalanced => 'Ausgewogen';

  @override
  String get levelBalancedHint =>
      'Verlustfrei und dazu: wiederholte Log-Zeilen werden zusammengefasst, JSON-Listen zu kompakten Tabellen und sehr lange Tool-Ausgaben behalten Anfang und Ende. Empfohlen für Agents.';

  @override
  String get levelAggressive => 'Aggressiv';

  @override
  String get levelAggressiveHint =>
      'Ausgewogen und dazu: ältere Tool-Ausgaben werden ausgeblendet und ältere lange Nachrichten gekürzt. Studien zeigen: Agents bleiben ähnlich genau bei halben Kosten. Deine letzte Nachricht wird nie gekürzt.';

  @override
  String get compressionEngines => 'Schritte';

  @override
  String get compressionEnginesHint =>
      'Jeder Schritt greift nur, wenn er die Anfrage wirklich verkleinert.';

  @override
  String get compressionLimits => 'Wann komprimiert wird';

  @override
  String get compressionMinTokens => 'Verlustbehaftete Schritte ab (Tokens)';

  @override
  String get compressionMinTokensHint =>
      'Kürzere Anfragen werden nur verlustfrei bereinigt.';

  @override
  String get compressionKeepRecent => 'Unveränderte letzte Nachrichten';

  @override
  String get compressionKeepRecentHint =>
      'Ältere Nachrichten dürfen gekürzt werden, diese nie.';

  @override
  String get compressionTarget => 'Token-Ziel (optional)';

  @override
  String get compressionTargetHint =>
      'Ist eine Anfrage noch größer, werden die ältesten Nachrichten weggelassen, bis sie passt.';

  @override
  String get engWhitespace => 'Leerzeichen bereinigen';

  @override
  String get engWhitespaceHint =>
      'Leerzeichen am Zeilenende und mehrfache Leerzeilen, außerhalb von Code.';

  @override
  String get engJsonMinify => 'JSON verkleinern';

  @override
  String get engJsonMinifyHint => 'Gleiche Daten ohne Einrückung.';

  @override
  String get engDedupe => 'Doppelte Blöcke streichen';

  @override
  String get engDedupeHint =>
      'Große, doppelt gesendete Texte bleiben einmal erhalten (der neueste).';

  @override
  String get engRepeatedLines => 'Wiederholte Zeilen zusammenfassen';

  @override
  String get engRepeatedLinesHint =>
      '„Zeile 40-mal wiederholt“ statt 40 gleicher Log-Zeilen.';

  @override
  String get engJsonTables => 'JSON-Listen als Tabellen';

  @override
  String get engJsonTablesHint =>
      'Listen von Objekten mit gleichen Feldern nennen jeden Feldnamen nur einmal.';

  @override
  String get engTrimToolOutput => 'Lange Tool-Ausgaben kürzen';

  @override
  String get engTrimToolOutputHint =>
      'Behält Anfang und Ende, wo meist Ergebnisse und Fehler stehen.';

  @override
  String get engMaskOldToolOutput => 'Ältere Tool-Ausgaben ausblenden';

  @override
  String get engMaskOldToolOutputHint =>
      'Nur die letzten drei Tool-Ergebnisse bleiben vollständig.';

  @override
  String get engCondenseHistory => 'Ältere Nachrichten kürzen';

  @override
  String get engCondenseHistoryHint =>
      'Lange ältere Nachrichten behalten Anfang und Ende.';

  @override
  String get engTokenTarget => 'Token-Ziel einhalten';

  @override
  String get engTokenTargetHint =>
      'Lässt bei Bedarf die ältesten Nachrichten weg.';

  @override
  String get compressionTryHint =>
      'Füge einen langen Text oder ein Log ein und sieh, wie viel die aktuelle Stufe spart.';

  @override
  String compressionTryResult(String before, String after) {
    return '$before → $after Tokens';
  }

  @override
  String get tryItTitle => 'Ausprobieren';

  @override
  String get tryItRun => 'Ergebnis zeigen';

  @override
  String get tryItSent => 'Was ein Anbieter bekäme';

  @override
  String get pgEmbedHint =>
      'Ein Text pro Zeile. NeoRouter macht aus jedem einen Vektor und zeigt, wie ähnlich sie sich sind.';

  @override
  String get pgEmbedRun => 'Vektoren erstellen';

  @override
  String pgEmbedResult(String count, String dims) {
    return '$count Vektoren mit je $dims Zahlen';
  }

  @override
  String get pgSimilarity => 'Ähnlichkeit';

  @override
  String pgPinned(String model) {
    return 'Dieser Key nutzt für Embeddings jetzt immer $model, damit deine Vektoren vergleichbar bleiben.';
  }

  @override
  String get pgImageHint => 'Beschreibe das Bild, das du möchtest.';

  @override
  String get pgImageRun => 'Bild erstellen';

  @override
  String get pgSize => 'Größe';

  @override
  String get pgSpeechHint => 'Schreibe, was vorgelesen werden soll.';

  @override
  String get pgSpeechRun => 'Vorlesen';

  @override
  String get pgVoice => 'Stimme';

  @override
  String get pgVoiceDefault => 'Standardstimme';

  @override
  String get pgPlay => 'Abspielen';

  @override
  String get pgStop => 'Stopp';

  @override
  String get pgDownload => 'Herunterladen';

  @override
  String get pgTranscribeHint =>
      'Wähle eine Aufnahme (bis 25 MB) und NeoRouter macht Text daraus.';

  @override
  String get pgChooseFile => 'Audiodatei wählen';

  @override
  String get pgUseSpeech => 'Vorgelesenen Text nehmen';

  @override
  String get pgTranscribeRun => 'Transkribieren';

  @override
  String get pgTranslate => 'Ins Englische übersetzen';

  @override
  String get pgCheckHint =>
      'Füge eine Nachricht ein. Ein System-One-Modell entscheidet in Sekundenbruchteilen, ob sie unbedenklich ist.';

  @override
  String get pgCheckRun => 'Prüfen';

  @override
  String get pgSafe => 'Unbedenklich';

  @override
  String get pgCheckSafety => 'Schädliche Inhalte';

  @override
  String get pgCheckInjection => 'Prompt-Angriffe';

  @override
  String get pgFlagged => 'Markiert';

  @override
  String get pgVideoHint =>
      'Beschreibe eine kurze Szene. Videos dauern einige Minuten und sind nie gratis.';

  @override
  String get pgVideoRun => 'Video erstellen';

  @override
  String pgSeconds(String count) {
    return '$count Sekunden';
  }

  @override
  String pgVideoStatus(String status) {
    return 'Status: $status';
  }

  @override
  String get pgNoModels =>
      'Noch keine deiner Verbindungen bietet diese Art von Modell an.';

  @override
  String get pgAddProvider => 'Anbieter hinzufügen';

  @override
  String get unitPrice => 'Preis pro Einheit (USD)';

  @override
  String get unitImage => 'pro Bild';

  @override
  String get unitSecond => 'pro Sekunde';

  @override
  String get unitMinute => 'pro Audiominute';

  @override
  String get unitMchar => 'pro Million Zeichen';

  @override
  String get unitSearch => 'pro Suche';

  @override
  String get pgSearchHint =>
      'Durchsuche das Web mit deinen Suchmaschinen. Chat-Modelle nutzen dieselben, wenn eine Anfrage die Websuche einschaltet.';

  @override
  String get pgSearchRun => 'Suchen';

  @override
  String get pgNoResults => 'Keine Treffer.';

  @override
  String get activityKind => 'Art';

  @override
  String unitPriceLabel(String price, String unit) {
    return '$price $unit';
  }

  @override
  String get unitKind => 'Abrechnung';

  @override
  String get navTeam => 'Team';

  @override
  String get teamTitle => 'Team';

  @override
  String get teamDescription =>
      'Lass andere beim Verwalten deines NeoRouter helfen. Du legst pro Person fest, was sie sehen und ändern darf. Alles, was sie anlegen, gehört weiter dir.';

  @override
  String get inviteSomeone => 'Jemanden einladen';

  @override
  String get teamMembersTitle => 'Personen in deinem Team';

  @override
  String get teamMembersHint =>
      'Sie arbeiten in deinem Bereich mit den Rechten, die du ihnen gibst.';

  @override
  String get noMembersTitle => 'Noch niemand dabei';

  @override
  String get noMembersMessage =>
      'Erstelle einen Einladungslink und schick ihn der Person. Sie braucht ein Konto auf diesem Server.';

  @override
  String get inviteLinksTitle => 'Offene Einladungslinks';

  @override
  String get inviteLinksHint =>
      'Wer einen offenen Link hat, kann beitreten. Widerrufe Links, die du nicht mehr brauchst.';

  @override
  String get joinTeamTitle => 'Team einer anderen Person';

  @override
  String get joinTeamLabel => 'Einladungslink bekommen?';

  @override
  String get joinTeamHint =>
      'Tritt bei, um beim NeoRouter dieser Person mitzuhelfen. Dein eigener Bereich bleibt, wie er ist.';

  @override
  String get useInviteLink => 'Einladungslink nutzen';

  @override
  String memberOfTitle(String name) {
    return 'Du bist im Team von $name';
  }

  @override
  String get memberOfHint =>
      'Wechsle in der Seitenleiste den Bereich, um dort zu arbeiten.';

  @override
  String get youMay => 'Du darfst';

  @override
  String openSpaceOf(String name) {
    return 'Bereich von $name öffnen';
  }

  @override
  String get leaveTeam => 'Team verlassen';

  @override
  String leaveTeamConfirm(String name) {
    return 'Team von $name verlassen? Du verlierst den Zugriff, bis du wieder eingeladen wirst. Was du dort angelegt hast, bleibt.';
  }

  @override
  String get editPermissions => 'Rechte ändern';

  @override
  String permissionsFor(String name) {
    return 'Was $name darf';
  }

  @override
  String get removeMember => 'Aus dem Team entfernen';

  @override
  String removeMemberConfirm(String name) {
    return '$name entfernen? Der Zugriff endet sofort. Keys und Anbieter, die die Person angelegt hat, bleiben.';
  }

  @override
  String get noPermissions => 'Noch nichts';

  @override
  String permissionCount(String count) {
    return '$count Rechte';
  }

  @override
  String get permView => 'Ansehen';

  @override
  String get permCreate => 'Erstellen';

  @override
  String get permEdit => 'Ändern';

  @override
  String get permDelete => 'Löschen';

  @override
  String get permActivityView => 'Aktivität und Kosten ansehen';

  @override
  String get permPlaygroundUse => 'Playground nutzen';

  @override
  String get permProvidersManage => 'Hinzufügen und ändern';

  @override
  String get permModelsManage => 'Modelle an- und ausschalten';

  @override
  String get presetView => 'Nur ansehen';

  @override
  String get presetKeys => 'API-Keys verwalten';

  @override
  String get presetFull => 'Alles';

  @override
  String get inviteLabel => 'Name';

  @override
  String get inviteLabelHint => 'Für wen ist der Link?';

  @override
  String get inviteExpiry => 'Link gilt';

  @override
  String get expiry24h => '24 Stunden';

  @override
  String get expiry7d => '7 Tage';

  @override
  String get expiry30d => '30 Tage';

  @override
  String get expiryNever => 'Bis zum Widerruf';

  @override
  String get inviteSingleUseLabel => 'Nur für eine Person';

  @override
  String get inviteSingleUseHint =>
      'Der Link funktioniert nicht mehr, sobald jemand beigetreten ist.';

  @override
  String get invitePermissionsLabel => 'Was die Person darf';

  @override
  String get createInviteLink => 'Link erstellen';

  @override
  String get inviteLinkTitle => 'Einladungslink bereit';

  @override
  String get inviteLinkBody =>
      'Schick diesen Link der Person. Er wird nur jetzt angezeigt; du kannst ihn jederzeit widerrufen.';

  @override
  String get inviteUntitled => 'Einladungslink';

  @override
  String get inviteSingleUse => 'Eine Person';

  @override
  String inviteReusable(String count) {
    return 'Mehrfach · $count× genutzt';
  }

  @override
  String get inviteNoExpiry => 'Ohne Ablauf';

  @override
  String inviteExpires(String date) {
    return 'Bis $date';
  }

  @override
  String get inviteLinkField => 'Einladungslink';

  @override
  String get inviteLinkFieldHint => 'Füge den erhaltenen Link ein';

  @override
  String get inviteCheck => 'Link prüfen';

  @override
  String invitePreview(String name) {
    return 'Dem Team von $name beitreten?';
  }

  @override
  String get invitePreviewHint =>
      'Du kannst dann in diesem Bereich mit diesen Rechten arbeiten:';

  @override
  String get joinTeam => 'Team beitreten';

  @override
  String get spaceLabel => 'Bereich';

  @override
  String get mySpace => 'Mein Bereich';

  @override
  String spaceOf(String name) {
    return 'Bereich von $name';
  }

  @override
  String get switchSpace => 'Bereich wechseln';

  @override
  String createdBy(String name) {
    return 'Erstellt von $name';
  }

  @override
  String get formerMember => 'einem früheren Mitglied';

  @override
  String inTeamOf(String name) {
    return 'im Team von $name';
  }

  @override
  String get teamActivityTitle => 'Team-Aktivität';

  @override
  String get teamActivityHint =>
      'Einladungen, Beitritte und geänderte Rechte auf dem ganzen Server.';

  @override
  String get teamActivityEmpty => 'Bisher ist nichts passiert.';

  @override
  String teamEventInviteCreate(String actor) {
    return '$actor hat einen Einladungslink erstellt';
  }

  @override
  String teamEventInviteRevoke(String actor) {
    return '$actor hat einen Einladungslink widerrufen';
  }

  @override
  String teamEventJoin(String actor, String subject) {
    return '$actor ist dem Team von $subject beigetreten';
  }

  @override
  String teamEventPermissions(String actor, String subject) {
    return '$actor hat die Rechte von $subject geändert';
  }

  @override
  String teamEventRelease(String actor, String subject) {
    return '$actor hat $subject aus dem Team entfernt';
  }

  @override
  String teamEventLeave(String actor, String subject) {
    return '$actor hat das Team von $subject verlassen';
  }
}
