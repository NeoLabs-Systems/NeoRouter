import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/provider_traffic_limits.dart';
import 'package:neorouter/src/backend_client.dart';
import 'package:neorouter/src/models.dart';

class _AccountClient extends BackendClient {
  String? savedPath;
  Object? savedBody;

  @override
  Future<Map<String, dynamic>> patch(String path, Object body) async {
    savedPath = path;
    savedBody = body;
    return {'account': <String, dynamic>{}};
  }
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  testWidgets('choose one connection and edit only its traffic limits', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 740);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = _AccountClient();
    ProviderAccount account(String id, int cap) => ProviderAccount({
      'id': id,
      'label': id,
      'slug': id,
      'definitionId': 'provider',
      'billingMode': 'paid',
      'limits': {'concurrent': cap},
    });
    await tester.pumpWidget(
      MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        localizationsDelegates: AppL10n.localizationsDelegates,
        supportedLocales: AppL10n.supportedLocales,
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => showDialog<bool>(
                context: context,
                builder: (_) => ProviderTrafficLimitsDialog(
                  controller: NeoRouterController(client: client),
                  accounts: [account('primary', 2), account('overflow', 4)],
                ),
              ),
              child: const Text('Open'),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();
    expect(find.byType(TextField), findsNothing);
    await tester.tap(find.text('overflow'));
    await tester.pumpAndSettle();
    final fields = find.byType(TextField);
    expect(tester.widget<TextField>(fields.first).controller!.text, '4');
    await tester.enterText(fields.first, '');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();
    expect(client.savedPath, '/api/v1/providers/accounts/overflow');
    expect(client.savedBody, {
      'limits': {'concurrent': null},
    });
    expect(find.byType(ProviderTrafficLimitsDialog), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
