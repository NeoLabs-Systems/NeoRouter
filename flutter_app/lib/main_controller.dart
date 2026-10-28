import 'package:flutter/material.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'src/backend_client.dart';
import 'src/models.dart';
import 'src/router_api.dart';

enum AppPage { home, playground, models, providers, keys, activity, routing, settings, admin }

/// Single root state object. Pages own their page-local data and talk to the
/// API through [api]; the controller holds session, navigation and prefs.
class NeoRouterController extends ChangeNotifier {
  NeoRouterController({BackendClient? client}) : client = client ?? BackendClient();

  static const String _tokenKey = 'neorouter.session';
  static const String _themeKey = 'neorouter.theme';
  static const String _localeKey = 'neorouter.locale';

  final BackendClient client;
  late final RouterApi api = RouterApi(client);

  bool bootstrapping = true;
  bool loading = false;
  String? authError;
  bool registrationOpen = false;
  bool bootstrapRegistration = false;
  AppUser? user;
  AppPage page = AppPage.home;
  ThemeMode themeMode = ThemeMode.system;
  Locale? locale;
  String? endpoint;

  /// Result of a provider OAuth redirect (e.g. OpenRouter connect), read once
  /// from the URL the browser came back to: 'connected' or an error message.
  String? oauthStatus;
  String? oauthError;

  /// Set between the password step and the authenticator-code step.
  String? twoFactorChallenge;

  bool get hasUser => user != null;
  bool get awaitingTwoFactor => twoFactorChallenge != null;

  Future<void> bootstrap() async {
    final prefs = await SharedPreferences.getInstance();
    themeMode = ThemeMode.values.firstWhere((mode) => mode.name == prefs.getString(_themeKey), orElse: () => ThemeMode.system);
    final savedLocale = prefs.getString(_localeKey);
    locale = savedLocale == null ? null : Locale(savedLocale);
    final token = prefs.getString(_tokenKey);
    if (token != null) {
      client.setToken(token);
      try {
        user = await api.me();
        await _loadInfo();
      } on ApiException {
        client.setToken(null);
        await prefs.remove(_tokenKey);
      } catch (_) {
        // Server unreachable: keep the token, show the sign-in screen.
      }
    }
    _readOAuthResult();
    await refreshRegistration();
    bootstrapping = false;
    notifyListeners();
  }

  void _readOAuthResult() {
    final params = Uri.base.queryParameters;
    if (params['oauth'] == null) return;
    oauthStatus = params['oauth_status'];
    oauthError = params['oauth_error'];
    if (user != null) page = AppPage.providers;
  }

  void consumeOAuthResult() {
    oauthStatus = null;
    oauthError = null;
  }

  Future<void> refreshRegistration() async {
    try {
      final status = await api.registrationStatus();
      registrationOpen = status['open'] == true;
      bootstrapRegistration = status['bootstrap'] == true;
    } catch (_) {
      registrationOpen = false;
    }
  }

  Future<void> _loadInfo() async {
    try {
      endpoint = (await api.info())['endpoint'] as String?;
    } catch (_) {
      endpoint = null;
    }
  }

  Future<void> _completeSession(Map<String, dynamic> result) async {
    final session = Map<String, dynamic>.from(result['session'] as Map);
    final token = session['token'] as String;
    client.setToken(token);
    (await SharedPreferences.getInstance()).setString(_tokenKey, token);
    user = AppUser(Map<String, dynamic>.from(result['user'] as Map));
    page = AppPage.home;
    await _loadInfo();
  }

  Future<bool> login(String account, String password) => _authAction(() async {
        final result = await api.login(account, password);
        if (result['twoFactorRequired'] == true) {
          twoFactorChallenge = result['challenge'] as String;
          return;
        }
        await _completeSession(result);
      });

  Future<bool> completeTwoFactor(String code) => _authAction(() async {
        await _completeSession(await api.loginTwoFactor(twoFactorChallenge!, code));
        twoFactorChallenge = null;
      });

  void cancelTwoFactor() {
    twoFactorChallenge = null;
    authError = null;
    notifyListeners();
  }

  Future<bool> register(String username, String email, String password) => _authAction(() async {
        await _completeSession(await api.register(username, email, password));
      });

  Future<bool> _authAction(Future<void> Function() action) async {
    loading = true;
    authError = null;
    notifyListeners();
    try {
      await action();
      return true;
    } on ApiException catch (error) {
      authError = error.message;
      return false;
    } catch (_) {
      authError = 'The server could not be reached.';
      return false;
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  void showAuthError(String message) {
    authError = message;
    notifyListeners();
  }

  Future<void> logout() async {
    try {
      await api.logout();
    } catch (_) {}
    client.setToken(null);
    (await SharedPreferences.getInstance()).remove(_tokenKey);
    user = null;
    await refreshRegistration();
    notifyListeners();
  }

  void updateUser(AppUser next) {
    user = next;
    notifyListeners();
  }

  void go(AppPage next) {
    page = next;
    notifyListeners();
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    themeMode = mode;
    notifyListeners();
    (await SharedPreferences.getInstance()).setString(_themeKey, mode.name);
  }

  Future<void> setLocale(Locale? next) async {
    locale = next;
    notifyListeners();
    final prefs = await SharedPreferences.getInstance();
    if (next == null) {
      await prefs.remove(_localeKey);
    } else {
      await prefs.setString(_localeKey, next.languageCode);
    }
  }
}
