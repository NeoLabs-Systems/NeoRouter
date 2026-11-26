import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoAuthPost;

import '../backend_client.dart';

/// A held status request outlasts the server's 25 second long poll.
const Duration _longPollTimeout = Duration(seconds: 45);

/// Runs the shared NeoLabs auth endpoints over NeoRouter's HTTP client. The
/// shared client names `/api/...`; NeoRouter serves `/api/v1/...`.
NeoAuthPost neoAuthPost(BackendClient client) => (baseUrl, path, body, {allowUnauthorized = false}) {
  final route = path.replaceFirst('/api/', '/api/v1/');
  return body.containsKey('knownStatus') ? client.postHeld(route, body, _longPollTimeout) : client.post(route, body);
};
