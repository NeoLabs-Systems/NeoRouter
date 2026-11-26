import 'package:web/web.dart' as web;

/// Rewrites the address bar without loading the page again.
void replaceAddress(Uri uri) => web.window.history.replaceState(null, '', uri.toString());
