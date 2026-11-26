/// The sign-in request a scanned code opened this web app with
/// (`/app/?qr_login=1&challenge=…&secret=…`).
class QrApprovalLink {
  const QrApprovalLink({required this.challengeId, required this.secret});

  static const List<String> parameters = <String>['qr_login', 'challenge', 'secret'];

  static QrApprovalLink? from(Uri uri) {
    final query = uri.queryParameters;
    final challengeId = query['challenge'] ?? '';
    final secret = query['secret'] ?? '';
    if (query['qr_login'] != '1' || challengeId.isEmpty || secret.isEmpty) return null;
    return QrApprovalLink(challengeId: challengeId, secret: secret);
  }

  /// [uri] without the sign-in request, so a reload or a bookmark drops it.
  static Uri strip(Uri uri) {
    final query = Map<String, String>.of(uri.queryParameters)..removeWhere((key, _) => parameters.contains(key));
    return Uri(
      scheme: uri.scheme,
      userInfo: uri.userInfo,
      host: uri.host,
      port: uri.hasPort ? uri.port : null,
      path: uri.path,
      queryParameters: query.isEmpty ? null : query,
      fragment: uri.hasFragment ? uri.fragment : null,
    );
  }

  final String challengeId;
  final String secret;
}
