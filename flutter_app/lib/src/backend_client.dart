import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

class ApiException implements Exception {
  ApiException(this.message, {this.code, this.status});

  final String message;
  final String? code;
  final int? status;

  @override
  String toString() => message;
}

class BackendClient {
  BackendClient({String? baseUrl}) : _baseUrl = _normalize(baseUrl ?? defaultBaseUrl);

  static const String configuredBaseUrl = String.fromEnvironment('NEOROUTER_API_BASE');

  static String get defaultBaseUrl {
    final configured = configuredBaseUrl.trim();
    if (kIsWeb) {
      if (configured.isEmpty) return '';
      final configuredHost = Uri.tryParse(configured)?.host ?? '';
      if (_isLoopback(configuredHost) && !_isLoopback(Uri.base.host)) return '';
      return _normalize(configured);
    }
    return configured.isEmpty ? '' : _normalize(configured);
  }

  String _baseUrl;
  String? _token;

  String get baseUrl => _baseUrl;
  String? get token => _token;

  /// Origin used for HTTP and WebSocket. Empty on web means same-origin / relative.
  String get resolvedOrigin {
    if (_baseUrl.isNotEmpty) return _baseUrl;
    if (kIsWeb && Uri.base.host.isNotEmpty) {
      return Uri(
        scheme: Uri.base.scheme.isEmpty ? 'http' : Uri.base.scheme,
        host: Uri.base.host,
        port: Uri.base.hasPort ? Uri.base.port : null,
      ).toString().replaceFirst(RegExp(r'/$'), '');
    }
    return '';
  }

  static String _normalize(String value) => value.replaceAll(RegExp(r'/$'), '');

  static bool _isLoopback(String host) {
    final normalized = host.trim().toLowerCase();
    return normalized.isEmpty ||
        normalized == 'localhost' ||
        normalized == '127.0.0.1' ||
        normalized == '::1';
  }

  void setBaseUrl(String value) {
    _baseUrl = _normalize(value);
  }

  void setToken(String? token) {
    _token = token;
  }

  Uri _uri(String path) {
    if (_baseUrl.isEmpty) return Uri.parse(path);
    return Uri.parse('$_baseUrl$path');
  }

  Map<String, String> _headers({bool json = true}) {
    return <String, String>{
      if (json) 'Content-Type': 'application/json',
      if (_token != null) 'Authorization': 'Bearer $_token',
    };
  }

  Future<dynamic> _send(
    String method,
    String path, {
    Object? body,
    bool decode = true,
  }) async {
    final uri = _uri(path);
    final encoded = body == null || body is List<int> ? null : jsonEncode(body);
    final headers = _headers(json: encoded != null);
    late final http.Response response;
    switch (method) {
      case 'GET':
        response = await http.get(uri, headers: headers);
      case 'POST':
        response = body is List<int>
            ? await http.post(uri, headers: _headers(json: false)..['Content-Type'] = 'application/octet-stream', body: body)
            : await http.post(uri, headers: headers, body: encoded);
      case 'PUT':
        response = body is List<int>
            ? await http.put(uri, headers: _headers(json: false)..['Content-Type'] = 'application/octet-stream', body: body)
            : await http.put(uri, headers: headers, body: encoded);
      case 'PATCH':
        response = await http.patch(uri, headers: headers, body: encoded);
      case 'DELETE':
        response = await http.delete(uri, headers: headers, body: encoded);
      default:
        throw ArgumentError.value(method, 'method');
    }
    if (response.statusCode >= 400) {
      Map<String, dynamic>? payload;
      try {
        payload = jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
      } catch (_) {}
      final error = payload?['error'] as Map<String, dynamic>?;
      throw ApiException(
        (error?['message'] as String?) ?? 'Request failed (${response.statusCode}).',
        code: error?['code'] as String?,
        status: response.statusCode,
      );
    }
    if (!decode || response.body.isEmpty || response.statusCode == 204) {
      return response.bodyBytes;
    }
    return jsonDecode(utf8.decode(response.bodyBytes));
  }

  Future<Map<String, dynamic>> post(String path, [Object? body]) async {
    return Map<String, dynamic>.from(await _send('POST', path, body: body) as Map);
  }

  Future<Map<String, dynamic>> get(String path) async {
    return Map<String, dynamic>.from(await _send('GET', path) as Map);
  }

  Future<Map<String, dynamic>> patch(String path, Object body) async {
    return Map<String, dynamic>.from(await _send('PATCH', path, body: body) as Map);
  }

  Future<void> delete(String path, [Object? body]) async {
    await _send('DELETE', path, body: body, decode: false);
  }

  Future<void> put(String path, Object body) async {
    await _send('PUT', path, body: body, decode: false);
  }
}
