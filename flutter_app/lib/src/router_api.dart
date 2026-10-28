import 'backend_client.dart';
import 'capabilities.dart';
import 'models.dart';

/// Typed calls against the NeoRouter management API (/api/v1).
class RouterApi {
  RouterApi(this.client);

  final BackendClient client;

  static const String _base = '/api/v1';

  // ---- auth ---------------------------------------------------------------
  Future<Map<String, dynamic>> registrationStatus() => client.get('$_base/auth/registration');
  Future<Map<String, dynamic>> login(String account, String password) =>
      client.post('$_base/auth/login', <String, Object>{'account': account, 'password': password});
  Future<Map<String, dynamic>> register(String username, String email, String password) => client.post(
        '$_base/auth/register',
        <String, Object>{'username': username, 'email': email, 'password': password},
      );
  Future<Map<String, dynamic>> loginTwoFactor(String challenge, String code) =>
      client.post('$_base/auth/login/2fa', <String, Object>{'challenge': challenge, 'code': code});
  Future<Map<String, dynamic>> twoFactorStatus() => client.get('$_base/auth/2fa');
  Future<Map<String, dynamic>> twoFactorSetup(String currentPassword) =>
      client.post('$_base/auth/2fa/setup', <String, Object>{'currentPassword': currentPassword});
  Future<Map<String, dynamic>> twoFactorEnable(String code) => client.post('$_base/auth/2fa/enable', <String, Object>{'code': code});
  Future<Map<String, dynamic>> twoFactorDisable(String currentPassword, String code) =>
      client.post('$_base/auth/2fa/disable', <String, Object>{'currentPassword': currentPassword, 'code': code});
  Future<Map<String, dynamic>> twoFactorRecoveryCodes(String currentPassword, String code) =>
      client.post('$_base/auth/2fa/recovery-codes', <String, Object>{'currentPassword': currentPassword, 'code': code});
  Future<AppUser> me() async => AppUser(Map<String, dynamic>.from((await client.get('$_base/auth/me'))['user'] as Map));
  Future<void> logout() => client.post('$_base/auth/logout');
  Future<void> changePassword(String current, String next) =>
      client.put('$_base/auth/password', <String, Object>{'currentPassword': current, 'newPassword': next});
  Future<AppUser> updateProfile({String? displayName, String? email}) async => AppUser(Map<String, dynamic>.from(
        (await client.patch('$_base/auth/profile', <String, Object>{
          if (displayName != null) 'displayName': displayName,
          if (email != null) 'email': email,
        }))['user'] as Map,
      ));
  Future<Map<String, dynamic>> info() => client.get('$_base/info');

  // ---- providers ------------------------------------------------------------
  Future<List<ProviderDefinition>> definitions() async => _list(await client.get('$_base/providers/definitions'), 'definitions', ProviderDefinition.new);
  Future<List<ProviderAccount>> accounts() async => _list(await client.get('$_base/providers/accounts'), 'accounts', ProviderAccount.new);
  Future<Map<String, dynamic>> probeProvider(Map<String, Object?> body) => client.post('$_base/providers/probe', _clean(body));
  Future<String> startOpenRouterConnect(String returnTo) async =>
      (await client.post('$_base/oauth/openrouter/start', <String, Object>{'returnTo': returnTo}))['url'] as String;
  Future<Map<String, dynamic>> createAccount(Map<String, Object?> body) => client.post('$_base/providers/accounts', _clean(body));
  Future<ProviderAccount> updateAccount(String id, Map<String, Object?> body) async =>
      ProviderAccount(Map<String, dynamic>.from((await client.patch('$_base/providers/accounts/$id', _clean(body)))['account'] as Map));
  Future<void> deleteAccount(String id) => client.delete('$_base/providers/accounts/$id');
  Future<Map<String, dynamic>> validateAccount(String id) => client.post('$_base/providers/accounts/$id/validate');
  Future<Map<String, dynamic>> syncAccount(String id) => client.post('$_base/providers/accounts/$id/sync');
  Future<Map<String, dynamic>> probeQuota(String id) => client.post('$_base/providers/accounts/$id/quota');

  // ---- catalog ---------------------------------------------------------------
  Future<List<CatalogModel>> models({String capability = 'chat'}) async =>
      _list(await client.get('$_base/models?capability=$capability'), 'models', CatalogModel.new);
  Future<List<CapabilitySummary>> capabilities() async =>
      sortCapabilities(_list(await client.get('$_base/models/capabilities'), 'capabilities', CapabilitySummary.new));
  Future<CatalogModel> updateModel(int ref, {bool? enabled, Map<String, Object?>? overrides}) async => CatalogModel(Map<String, dynamic>.from(
        (await client.patch('$_base/models/$ref', <String, Object>{
          if (enabled != null) 'enabled': enabled,
          if (overrides != null) 'overrides': overrides,
        }))['model'] as Map,
      ));
  Future<CatalogModel> resetModelHealth(int ref) async =>
      CatalogModel(Map<String, dynamic>.from((await client.post('$_base/models/$ref/reset-health'))['model'] as Map));
  Future<void> bulkEnable({String? accountId, required bool enabled, bool onlyFree = false}) => client.post(
        '$_base/models/bulk',
        <String, Object>{if (accountId != null) 'accountId': accountId, 'enabled': enabled, 'onlyFree': onlyFree},
      );

  // ---- router keys ----------------------------------------------------------
  Future<List<RouterKey>> keys() async => _list(await client.get('$_base/keys'), 'keys', RouterKey.new);
  Future<({RouterKey key, String secret})> createKey(Map<String, Object?> body) async {
    final result = await client.post('$_base/keys', _clean(body));
    return (key: RouterKey(Map<String, dynamic>.from(result['key'] as Map)), secret: result['secret'] as String);
  }

  Future<RouterKey> updateKey(String id, Map<String, Object?> body) async =>
      RouterKey(Map<String, dynamic>.from((await client.patch('$_base/keys/$id', body))['key'] as Map));
  Future<void> revokeKey(String id) => client.post('$_base/keys/$id/revoke');
  Future<void> deleteKey(String id) => client.delete('$_base/keys/$id');

  // ---- routing ----------------------------------------------------------------
  Future<List<RoutingProfile>> profiles() async => _list(await client.get('$_base/routing/profiles'), 'profiles', RoutingProfile.new);
  Future<RoutingProfile> createProfile(String name, RoutingWeights weights, Map<String, Object?> constraints) async => RoutingProfile(
        Map<String, dynamic>.from((await client.post('$_base/routing/profiles', <String, Object>{
          'name': name,
          'weights': weights.toJson(),
          'constraints': _clean(constraints),
        }))['profile'] as Map),
      );
  Future<RoutingProfile> updateProfileWeights(String id, {String? name, RoutingWeights? weights, Map<String, Object?>? constraints}) async =>
      RoutingProfile(Map<String, dynamic>.from((await client.patch('$_base/routing/profiles/$id', <String, Object>{
        if (name != null) 'name': name,
        if (weights != null) 'weights': weights.toJson(),
        if (constraints != null) 'constraints': _clean(constraints),
      }))['profile'] as Map));
  Future<void> deleteProfile(String id) => client.delete('$_base/routing/profiles/$id');
  Future<Map<String, dynamic>> preview(Map<String, Object?> body) => client.post('$_base/routing/preview', _clean(body));

  // ---- usage ------------------------------------------------------------------
  Future<Map<String, dynamic>> analytics(String range) => client.get('$_base/usage/analytics?range=$range');
  Future<Map<String, dynamic>> summary(String range) => client.get('$_base/usage/summary?range=$range');
  Future<List<RequestLog>> logs({int limit = 100, String? status}) async =>
      _list(await client.get('$_base/usage/logs?limit=$limit${status == null ? '' : '&status=$status'}'), 'logs', RequestLog.new);
  Future<List<Map<String, dynamic>>> quotas() async =>
      ((await client.get('$_base/usage/quotas'))['accounts'] as List).map((item) => Map<String, dynamic>.from(item as Map)).toList();
  Future<Map<String, dynamic>> playground(Map<String, Object?> body) => client.post('$_base/playground/chat', _clean(body));

  // ---- admin --------------------------------------------------------------------
  Future<List<AppUser>> users() async => _list(await client.get('$_base/admin/users'), 'users', AppUser.new);
  Future<void> updateUser(String id, {bool? disabled, String? role}) => client.patch(
        '$_base/admin/users/$id',
        <String, Object>{if (disabled != null) 'disabled': disabled, if (role != null) 'role': role},
      );
  Future<void> deleteUser(String id) => client.delete('$_base/admin/users/$id');
  Future<Map<String, dynamic>> adminSettings() => client.get('$_base/admin/settings');
  Future<Map<String, dynamic>> updateAdminSettings(Map<String, Object?> body) => client.patch('$_base/admin/settings', body);
  Future<Map<String, dynamic>> refreshMetadata() => client.post('$_base/admin/metadata/refresh');

  static List<T> _list<T>(Map<String, dynamic> payload, String key, T Function(Map<String, dynamic>) build) =>
      (payload[key] as List? ?? const <Object>[]).map((item) => build(Map<String, dynamic>.from(item as Map))).toList();

  static Map<String, Object> _clean(Map<String, Object?> body) => <String, Object>{
        for (final entry in body.entries)
          if (entry.value != null) entry.key: entry.value!,
      };
}
