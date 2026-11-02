import 'package:web/web.dart' as web;

/// Loads the app again, e.g. after an update replaced the web client.
void reloadPage() => web.window.location.reload();
