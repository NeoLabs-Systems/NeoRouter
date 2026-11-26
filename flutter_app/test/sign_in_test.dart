import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_auth.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_qr_approve.dart';
import 'package:neorouter/main_settings_passkeys.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/auth/qr_link.dart';
import 'package:neorouter/src/auth/webauthn_client.dart';
import 'package:neorouter/src/backend_client.dart';

class _Passkeys implements WebAuthnClient {
  @override
  bool get isSupported => true;

  @override
  Future<Map<String, dynamic>> createCredential(Map<String, dynamic> options) async => <String, dynamic>{'id': 'new'};

  @override
  Future<Map<String, dynamic>> getAssertion(Map<String, dynamic> options) async => <String, dynamic>{'id': 'key'};
}

NeoRouterController _controller(List<String> calls, Map<String, Object> answers) => NeoRouterController(
  webAuthn: _Passkeys(),
  client: BackendClient(
    baseUrl: 'http://server',
    httpClient: MockClient((request) async {
      final call = '${request.method} ${request.url.path}';
      calls.add(call);
      return http.Response(jsonEncode(answers[call] ?? <String, Object>{}), 200);
    }),
  ),
);

Widget _app(Widget home) => MaterialApp(
  theme: buildNeoRouterTheme(Brightness.dark),
  locale: const Locale('en'),
  supportedLocales: AppL10n.supportedLocales,
  localizationsDelegates: const <LocalizationsDelegate<Object>>[
    AppL10n.delegate,
    GlobalMaterialLocalizations.delegate,
    GlobalWidgetsLocalizations.delegate,
    GlobalCupertinoLocalizations.delegate,
  ],
  home: home,
);

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  testWidgets('sign-in offers only what the server says it has', (tester) async {
    final controller = _controller(<String>[], <String, Object>{});
    await tester.pumpWidget(_app(NeoRouterAuthScreen(controller: controller)));
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('Forgot password?'), findsNothing);
    expect(find.text('Sign in with a passkey'), findsNothing);
    expect(find.text('Sign in with your phone'), findsNothing);
  });

  testWidgets('with reset mail the dialog sends the account', (tester) async {
    final calls = <String>[];
    final controller = _controller(calls, <String, Object>{
      'GET /api/v1/auth/options': <String, Object>{'registration': false, 'bootstrap': false, 'passwordReset': true},
    });
    await controller.refreshAuthOptions();
    await tester.pumpWidget(_app(NeoRouterAuthScreen(controller: controller)));
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('Sign in with a passkey'), findsOneWidget);
    expect(find.text('Sign in with your phone'), findsOneWidget);
    await tester.tap(find.text('Forgot password?'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).last, 'anna');
    await tester.tap(find.text('Send link'));
    await tester.pumpAndSettle();
    expect(calls, contains('POST /api/v1/auth/password/forgot'));
    expect(find.text('Reset password'), findsNothing);
  });

  testWidgets('a scanned code is approved only after a tap', (tester) async {
    final calls = <String>[];
    final controller = _controller(calls, <String, Object>{
      'POST /api/v1/account/qr-login/resolve': <String, Object>{
        'requestedDevice': <String, Object>{'label': 'Firefox on Linux'},
        'requestLocation': <String, Object>{'ipAddress': '10.0.0.7'},
      },
    })..pendingQrApproval = QrApprovalLink.from(Uri.parse('http://server/app/?qr_login=1&challenge=c&secret=s'));
    await tester.pumpWidget(_app(QrApproveScreen(controller: controller)));
    await tester.pumpAndSettle();
    expect(find.text('Firefox on Linux'), findsOneWidget);
    expect(find.text('10.0.0.7'), findsOneWidget);
    expect(calls, isNot(contains('POST /api/v1/account/qr-login/approve')));
    await tester.tap(find.text('Approve'));
    await tester.pumpAndSettle();
    expect(calls, contains('POST /api/v1/account/qr-login/approve'));
  });

  testWidgets('passkeys are listed from the server', (tester) async {
    final controller = _controller(<String>[], <String, Object>{
      'GET /api/v1/settings/security-keys': <String, Object>{
        'credentials': <Object>[
          <String, Object>{'id': 1, 'label': 'Laptop'},
        ],
      },
    });
    await tester.pumpWidget(_app(Scaffold(body: PasskeysSection(controller: controller))));
    await tester.pumpAndSettle();
    expect(find.text('Laptop'), findsOneWidget);
    expect(find.text('Add passkey'), findsOneWidget);
  });

  test('the sign-in request leaves the address once answered', () {
    final uri = Uri.parse('https://host/app/?qr_login=1&challenge=c&secret=s&page=keys');
    expect(QrApprovalLink.from(uri)?.challengeId, 'c');
    expect(QrApprovalLink.strip(uri).toString(), 'https://host/app/?page=keys');
  });
}
