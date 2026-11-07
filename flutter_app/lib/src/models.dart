// Typed views over the NeoRouter management API payloads.

double? _double(Object? value) => value is num ? value.toDouble() : null;
int? _int(Object? value) => value is num ? value.toInt() : null;
bool? _bool(Object? value) => value is bool ? value : null;
List<String> _strings(Object? value) => value is List ? value.map((item) => '$item').toList() : const <String>[];
Map<String, dynamic> _map(Object? value) => value is Map ? Map<String, dynamic>.from(value) : <String, dynamic>{};

class AppUser {
  AppUser(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get username => raw['username'] as String;
  String get email => raw['email'] as String? ?? '';
  String get displayName => raw['displayName'] as String? ?? username;
  bool get isAdmin => raw['role'] == 'admin';
  bool get disabled => raw['disabled'] == true;
  String get role => raw['role'] as String? ?? 'user';

  /// Admin list only: the owner whose team this account is in.
  TeamUser? get teamOwner => TeamUser.from(raw['teamOwner']);
}

/// Another account as teammates see it: name only, nothing private.
class TeamUser {
  TeamUser(this.raw);
  final Map<String, dynamic> raw;

  static TeamUser? from(Object? value) => value is Map ? TeamUser(Map<String, dynamic>.from(value)) : null;

  String get id => raw['id'] as String;
  String get username => raw['username'] as String;
  String get displayName => raw['displayName'] as String? ?? username;
}

class TeamMember {
  TeamMember(this.raw);
  final Map<String, dynamic> raw;

  TeamUser get user => TeamUser(_map(raw['user']));
  List<String> get permissions => _strings(raw['permissions']);
  String? get joinedAt => raw['joinedAt'] as String?;
}

class TeamInvite {
  TeamInvite(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get label => raw['label'] as String? ?? '';
  List<String> get permissions => _strings(raw['permissions']);
  bool get singleUse => raw['singleUse'] == true;
  int get useCount => _int(raw['useCount']) ?? 0;
  String? get expiresAt => raw['expiresAt'] as String?;
  String get status => raw['status'] as String? ?? 'active';
  bool get active => status == 'active';
}

/// The Team page in one payload: whose team this account is in, its own
/// members and links, and the permission catalog.
class TeamSummary {
  TeamSummary(this.raw);
  final Map<String, dynamic> raw;

  Map<String, dynamic>? get _memberOf => raw['memberOf'] is Map ? _map(raw['memberOf']) : null;
  TeamUser? get owner => TeamUser.from(_memberOf?['owner']);
  List<String> get grantedToMe => _strings(_memberOf?['permissions']);
  List<TeamMember> get members => (raw['members'] as List? ?? const <Object>[]).map((item) => TeamMember(_map(item))).toList();
  List<TeamInvite> get invites => (raw['invites'] as List? ?? const <Object>[]).map((item) => TeamInvite(_map(item))).toList();
  List<String> get permissions => _strings(raw['permissions']);
  Map<String, List<String>> get presets => _map(raw['presets']).map((key, value) => MapEntry(key, _strings(value)));
}

class ProviderField {
  ProviderField(this.raw);
  final Map<String, dynamic> raw;

  String get key => raw['key'] as String;
  String get label => raw['label'] as String;
  bool get required => raw['required'] != false;
  String? get placeholder => raw['placeholder'] as String?;
}

class ProviderDefinition {
  ProviderDefinition(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get label => raw['label'] as String;
  String get description => raw['description'] as String? ?? '';
  String get category => raw['category'] as String;
  String? get website => raw['website'] as String?;
  String? get keyUrl => raw['keyUrl'] as String?;
  String get baseUrl => raw['baseUrl'] as String? ?? '';
  bool get editableBaseUrl => raw['editableBaseUrl'] == true;
  bool get keyOptional => raw['keyOptional'] == true;
  // How the connection is made when it is not an API key (e.g. codex_device).
  String? get connect => raw['connect'] as String?;
  List<ProviderField> get fields => (raw['fields'] as List? ?? const <Object>[]).map((item) => ProviderField(_map(item))).toList();
  Map<String, dynamic> get billing => _map(raw['billing']);
  String get defaultBillingMode => billing['defaultMode'] as String? ?? 'paid';
  bool get freeTierAvailable => billing['freeTierAvailable'] == true;
  bool get hasAlwaysFreeModels => billing['hasAlwaysFreeModels'] == true;
  /// Only some models are free (a partial free tier or ":free" models).
  bool get freeTierPartial => billing['freeTierPartial'] == true;
  bool get planAvailable => billing['planAvailable'] == true;
  String? get planNote => billing['planNote'] as String?;
  String get billingNote => billing['note'] as String? ?? '';
  String? get billingSource => billing['source'] as String?;
  String? get billingCheckedAt => billing['checkedAt'] as String?;

  /// How using it is paid for, in one word: free, free_models, plan or payg.
  String get costKind {
    if (category == 'plan') return 'plan';
    if (freeTierAvailable && !freeTierPartial) return 'free';
    if (freeTierAvailable || hasAlwaysFreeModels) return 'free_models';
    return 'payg';
  }
}

class ProviderAccount {
  ProviderAccount(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get definitionId => raw['definitionId'] as String;
  String get providerLabel => raw['providerLabel'] as String? ?? definitionId;
  /// Accounts with the same key are one provider (two Groq keys: one Groq).
  String get providerKey => raw['providerKey'] as String? ?? definitionId;
  String get providerName => raw['providerName'] as String? ?? providerLabel;
  String? get category => raw['category'] as String?;
  String get slug => raw['slug'] as String;
  String get label => raw['label'] as String;
  String? get baseUrl => raw['baseUrl'] as String?;
  String? get keyPreview => raw['keyPreview'] as String?;
  String get billingMode => raw['billingMode'] as String;
  String get billingState => raw['billingState'] as String? ?? 'unknown';
  String? get billingMessage => raw['billingMessage'] as String?;
  bool get billingVerifiable => raw['billingVerifiable'] == true;
  bool get enabled => raw['enabled'] == true;
  String get status => raw['status'] as String? ?? 'unknown';
  String? get statusMessage => raw['statusMessage'] as String?;
  String? get lastValidatedAt => raw['lastValidatedAt'] as String?;
  String? get lastSyncedAt => raw['lastSyncedAt'] as String?;
  int get modelCount => _int(_map(raw['models'])['total']) ?? 0;
  int get freeModelCount => _int(_map(raw['models'])['free']) ?? 0;
  /// The pools its free models are shared in.
  List<String> get sharedInPools => _strings(raw['sharedInPools']);
  /// "Free models only" can be chosen (the provider or its listing has free models).
  bool get freeAvailable => raw['freeAvailable'] == true;
  bool get freeTierAll => raw['freeTierAll'] == true;
  int get enabledModelCount => _int(_map(raw['models'])['enabled']) ?? 0;
  TeamUser? get createdBy => TeamUser.from(raw['createdBy']);
}

class CatalogModel {
  CatalogModel(this.raw);
  final Map<String, dynamic> raw;

  int get ref => _int(raw['ref'])!;
  String get id => raw['id'] as String;
  String get modelId => raw['modelId'] as String;
  String get name => raw['name'] as String? ?? modelId;
  String get providerSlug => raw['providerSlug'] as String;
  /// The model itself, whichever provider serves it ("beam" on Kilo and Ollama).
  String get canonicalId => raw['canonicalId'] as String? ?? modelId;
  /// The provider, whichever of its accounts serves this copy.
  String get providerKey => raw['providerKey'] as String? ?? providerSlug;
  String get providerName => raw['providerName'] as String? ?? providerSlug;
  /// Live models: the WebSocket protocols that reach them.
  List<String> get liveProtocols => _strings(raw['liveProtocols']);
  String get definitionId => raw['definitionId'] as String;
  String get accountId => raw['accountId'] as String;
  /// Offered by a pool partner (providerName is the partner).
  bool get fromPool => definitionId == 'neorouter_peer';
  bool get enabled => raw['enabled'] == true;
  bool get isFree => raw['isFree'] == true;
  bool get isPlan => raw['isPlan'] == true;
  String? get freeReason => raw['freeReason'] as String?;
  int? get contextWindow => _int(raw['contextWindow']);
  bool? get tools => _bool(raw['tools']);
  bool? get vision => _bool(raw['vision']);
  bool? get reasoning => _bool(raw['reasoning']);
  double? get priceIn => _double(raw['priceIn']);
  double? get priceOut => _double(raw['priceOut']);
  String get capability => raw['capability'] as String? ?? 'chat';

  /// Media models: USD per image, second (video), minute (audio in) or
  /// million characters (speech).
  String? get priceUnit => raw['priceUnit'] as String?;
  double? get unitPrice => _double(raw['unitPrice']);
  String? get priceSource => raw['priceSource'] as String?;
  double? get intelligence => _double(raw['intelligence']);
  String? get intelligenceSource => raw['intelligenceSource'] as String?;
  double get reliability => _double(raw['reliability']) ?? 0;
  double get speedScore => _double(raw['speedScore']) ?? 0;
  double get costScore => _double(raw['costScore']) ?? 0;
  Map<String, dynamic> get stats => _map(raw['stats']);
  Map<String, dynamic> get availability => _map(raw['availability']);
  Map<String, dynamic> get overrides => _map(raw['overrides']);
  bool get available => availability['available'] != false;
  String? get unavailableReason => availability['reason'] as String?;
  int? get unavailableUntil => _int(availability['until']);
  double get penalty => _double(stats['penalty']) ?? 0;
  double? get latencyMs => _double(stats['latencyMs']);
  double? get ttftMs => _double(stats['ttftMs']);
  double? get tps => _double(stats['tps']);
  int get samples => _int(stats['samples']) ?? 0;
  String? get lastError => stats['lastError'] as String?;
}

class RouterKey {
  RouterKey(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get name => raw['name'] as String;
  String get prefix => raw['prefix'] as String;
  String get mode => raw['mode'] as String;
  bool get allowFree => raw['allowFree'] != false;
  bool get allowPlan => raw['allowPlan'] != false;
  bool get allowPaid => raw['allowPaid'] == true || (raw['allowPaid'] == null && mode != 'free_only');
  double? get budgetUsd => _double(raw['budgetUsd']);
  String get budgetPeriod => raw['budgetPeriod'] as String? ?? 'total';
  double get spentUsd => _double(raw['spentUsd']) ?? 0;
  double? get remainingUsd => _double(raw['remainingUsd']);
  String? get periodResetsAt => raw['periodResetsAt'] as String?;
  // Budget pacing: ahead, on_track, tight or over (none for one-time budgets).
  String? get paceState => (raw['pacing'] as Map?)?['state'] as String?;
  double? get paceForecast => _double((raw['pacing'] as Map?)?['forecast']);
  int? get rpmLimit => _int(raw['rpmLimit']);
  // Who goes first when models are busy: high, normal or low.
  String get priority => raw['priority'] as String? ?? 'normal';
  int? get tokenLimit => _int(raw['tokenLimit']);
  double? get maxPricePerMillion => _double(raw['maxPricePerMillion']);
  /// Whether the key may also use pool partners' free models.
  bool get usesPool => raw['poolAccess'] == 'pool';
  String get tokenPeriod => raw['tokenPeriod'] as String? ?? 'monthly';
  int? get tokensUsed => _int(raw['tokensUsed']);
  String? get tokenResetsAt => raw['tokenResetsAt'] as String?;
  List<String> get allowedModels => _strings(raw['allowedModels']);
  String get profile => raw['profile'] as String? ?? 'balanced';
  String? get expiresAt => raw['expiresAt'] as String?;
  bool get revoked => raw['revoked'] == true;
  String? get lastUsedAt => raw['lastUsedAt'] as String?;
  TeamUser? get createdBy => TeamUser.from(raw['createdBy']);
  // Set when an app received the key through "Connect with NeoRouter".
  String? get appName => (raw['app'] as Map?)?['name'] as String?;
  String? get appOrigin => (raw['app'] as Map?)?['origin'] as String?;
  bool get isConnectedApp => appName != null;
}

/// An app asking for a key ("Connect with NeoRouter").
class ConnectRequest {
  ConnectRequest(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get name => raw['name'] as String;
  bool get isDevice => raw['kind'] == 'device';
  String? get host => (raw['redirect'] as Map?)?['host'] as String?;
  bool get secure => (raw['redirect'] as Map?)?['secure'] != false;
  List<String> get scopes => _strings(raw['scopes']);
}

class RoutingWeights {
  RoutingWeights({required this.intelligence, required this.speed, required this.reliability, required this.cost});

  factory RoutingWeights.fromJson(Map<String, dynamic> raw) => RoutingWeights(
    intelligence: _double(raw['intelligence']) ?? 0,
    speed: _double(raw['speed']) ?? 0,
    reliability: _double(raw['reliability']) ?? 0,
    cost: _double(raw['cost']) ?? 0,
  );

  double intelligence;
  double speed;
  double reliability;
  double cost;

  Map<String, double> toJson() => <String, double>{'intelligence': intelligence, 'speed': speed, 'reliability': reliability, 'cost': cost};

  RoutingWeights copy() => RoutingWeights(intelligence: intelligence, speed: speed, reliability: reliability, cost: cost);
}

class RoutingProfile {
  RoutingProfile(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get name => raw['name'] as String;
  bool get preset => raw['preset'] == true;
  /// The readable name used in model ids ("auto:paid-first").
  String get slug => raw['slug'] as String? ?? id;
  RoutingWeights get weights => RoutingWeights.fromJson(_map(raw['weights']));
  Map<String, dynamic> get constraints => _map(raw['constraints']);
  TeamUser? get createdBy => TeamUser.from(raw['createdBy']);
}

class RequestLog {
  RequestLog(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  DateTime get createdAt => DateTime.fromMillisecondsSinceEpoch(_int(raw['createdAt']) ?? 0);
  String? get keyName => raw['keyName'] as String?;
  String get apiFormat => raw['apiFormat'] as String? ?? '';
  String get requestedModel => raw['requestedModel'] as String? ?? '';
  String? get model => raw['model'] as String?;
  String get capability => raw['capability'] as String? ?? 'chat';
  /// The kind of task routing recognised (chat only).
  String? get task => raw['task'] as String?;
  bool get success => raw['status'] == 'success';
  int? get httpStatus => _int(raw['httpStatus']);
  String? get errorCode => raw['errorCode'] as String?;
  String? get errorMessage => raw['errorMessage'] as String?;
  int get inputTokens => _int(raw['inputTokens']) ?? 0;
  int get outputTokens => _int(raw['outputTokens']) ?? 0;
  double get costUsd => _double(raw['costUsd']) ?? 0;
  bool get isFree => raw['isFree'] == true;
  int? get latencyMs => _int(raw['latencyMs']);
  int? get ttftMs => _int(raw['ttftMs']);
  int get attempts => _int(raw['attempts']) ?? 1;
  /// How long the request waited for a model to free up first.
  int get queuedMs => _int(raw['queuedMs']) ?? 0;
  bool get stream => raw['stream'] == true;
  List<Map<String, dynamic>> get trail => (raw['trail'] as List? ?? const <Object>[]).map(_map).toList();
  /// Why the request stayed off the pool (very sensitive content), or null.
  String? get keptPrivate => raw['keptPrivate'] as String?;
}

/// Give and take with one partner (requests and tokens).
class PoolTraffic {
  PoolTraffic(this.raw);
  final Map<String, dynamic> raw;

  int get givenRequests => _int(_map(raw['given'])['requests']) ?? 0;
  int get takenRequests => _int(_map(raw['taken'])['requests']) ?? 0;
  int get givenTokens => _int(_map(raw['given'])['tokens']) ?? 0;
  int get takenTokens => _int(_map(raw['taken'])['tokens']) ?? 0;
}

class PoolPartner {
  PoolPartner(this.raw);
  final Map<String, dynamic> raw;

  String get memberId => raw['memberId'] as String;
  String get name => raw['name'] as String? ?? '';
  bool get isHost => raw['isHost'] == true;
  String? get linkId => raw['linkId'] as String?;
  /// connecting, online, offline or paused.
  String get state => raw['state'] as String? ?? 'connecting';
  String? get problem => raw['problem'] as String?;
  int get models => _int(raw['models']) ?? 0;
  /// new, balanced, giving_more or taking_more.
  String get balance => _map(raw['balance'])['state'] as String? ?? 'new';
  PoolTraffic get week => PoolTraffic(_map(_map(raw['balance'])['week']));
  PoolTraffic get today => PoolTraffic(_map(raw['today']));
}

class ShareableAccount {
  ShareableAccount(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get label => raw['label'] as String? ?? '';
  String get definitionId => raw['definitionId'] as String? ?? '';
  int get freeModels => _int(raw['freeModels']) ?? 0;
  /// Subscription and pay-as-you-go models: shared only at a level the owner picks.
  int get planModels => _int(raw['planModels']) ?? 0;
  int get paidModels => _int(raw['paidModels']) ?? 0;
  /// The provider reports a balance: partner use spends it instead of being billed.
  bool get hasBalance => raw['hasBalance'] == true;
}

class PoolShare {
  PoolShare(this.raw);
  final Map<String, dynamic> raw;

  int get requestsPerDay => _int(raw['requestsPerDay']) ?? 300;
  int get tokensPerDay => _int(raw['tokensPerDay']) ?? 600000;
  int get concurrent => _int(raw['concurrent']) ?? 2;
  double get reserve => _double(raw['reserve']) ?? 0.3;
}

/// One pool this person is in.
class PoolInfo {
  PoolInfo(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get name => raw['name'] as String? ?? '';
  bool get isHost => raw['isHost'] == true;
  bool get paused => raw['paused'] == true;
  String get selfName => raw['selfName'] as String? ?? '';
  List<PoolPartner> get partners => (raw['partners'] as List? ?? const <Object>[]).map((item) => PoolPartner(_map(item))).toList();
  int get onlinePartners => partners.where((partner) => partner.state == 'online').length;
  List<String> get sharedAccountIds => _strings(raw['sharedAccountIds']);
  /// How much of each shared connection this pool gets: free, plan or paid.
  Map<String, String> get shareLevels => _map(raw['shareLevels']).map((key, value) => MapEntry(key, '$value'));
  List<Map<String, dynamic>> get invites => (raw['invites'] as List? ?? const <Object>[]).map(_map).toList();
  PoolShare get share => PoolShare(_map(raw['share']));
}

/// All pools of a space, with what can be shared and the privacy setting.
class PoolsSummary {
  PoolsSummary(this.raw);
  final Map<String, dynamic> raw;

  List<PoolInfo> get pools => (raw['pools'] as List? ?? const <Object>[]).map((item) => PoolInfo(_map(item))).toList();
  bool get inPool => pools.isNotEmpty;
  PoolInfo? pool(String? id) => pools.where((item) => item.id == id).firstOrNull;
  /// The pool just created or joined, when the server says.
  String? get createdId => raw['poolId'] as String?;
  List<ShareableAccount> get shareable => (raw['shareable'] as List? ?? const <Object>[]).map((item) => ShareableAccount(_map(item))).toList();
  /// Connections with free models: what the ordinary sharing picker offers.
  List<ShareableAccount> get shareableFree => shareable.where((account) => account.freeModels > 0).toList();
  bool get keepSensitivePrivate => _map(raw['privacy'])['keepSensitivePrivate'] != false;
  int get maxPools => _int(raw['maxPools']) ?? 10;
}
