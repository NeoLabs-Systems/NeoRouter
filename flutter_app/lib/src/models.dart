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
  bool get planAvailable => billing['planAvailable'] == true;
  String? get planNote => billing['planNote'] as String?;
  String get billingNote => billing['note'] as String? ?? '';
}

class ProviderAccount {
  ProviderAccount(this.raw);
  final Map<String, dynamic> raw;

  String get id => raw['id'] as String;
  String get definitionId => raw['definitionId'] as String;
  String get providerLabel => raw['providerLabel'] as String? ?? definitionId;
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
  String get definitionId => raw['definitionId'] as String;
  String get accountId => raw['accountId'] as String;
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
  List<String> get allowedModels => _strings(raw['allowedModels']);
  String get profile => raw['profile'] as String? ?? 'balanced';
  String? get expiresAt => raw['expiresAt'] as String?;
  bool get revoked => raw['revoked'] == true;
  String? get lastUsedAt => raw['lastUsedAt'] as String?;
  TeamUser? get createdBy => TeamUser.from(raw['createdBy']);
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
  bool get stream => raw['stream'] == true;
  List<Map<String, dynamic>> get trail => (raw['trail'] as List? ?? const <Object>[]).map(_map).toList();
}
