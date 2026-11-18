import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_routing_groups.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/backend_client.dart';

class _RoutingClient extends BackendClient {
  final List<Map<String, dynamic>> groups = [];
  Object? lastPreview;

  @override
  Future<Map<String, dynamic>> get(String path) async => switch (path) {
    '/api/v1/routing/groups' => {'groups': groups},
    '/api/v1/routing/profiles' => {'profiles': []},
    '/api/v1/providers/accounts' => {
      'accounts': [
        {'id': 'a', 'label': 'My subscription', 'definitionId': 'provider'},
      ],
    },
    _ => throw StateError(path),
  };

  @override
  Future<Map<String, dynamic>> post(String path, [Object? body]) async {
    if (path.endsWith('/groups')) {
      groups.add({...body as Map<String, dynamic>, 'id': 'work'});
      return {'group': groups.last};
    }
    if (path.endsWith('/preview')) {
      lastPreview = body;
      return {
        'selection': {'model': 'provider/model', 'group': 'work'},
      };
    }
    throw StateError(path);
  }
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);
  testWidgets('create and preview a routing alias on a narrow screen', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 750);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final client = _RoutingClient();
    await tester.pumpWidget(
      MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        localizationsDelegates: AppL10n.localizationsDelegates,
        supportedLocales: AppL10n.supportedLocales,
        home: Scaffold(
          body: RoutingGroupsScreen(
            controller: NeoRouterController(client: client),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(Icons.add));
    await tester.pumpAndSettle();
    expect(find.text('Allow per-use charges'), findsNothing);
    await tester.enterText(find.byType(TextField), 'My work');
    await tester.tap(find.byType(FilterChip));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Automatic'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('My subscription · provider').last);
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();
    expect(client.groups.single['name'], 'my-work');
    expect(client.groups.single['preferredAccount'], 'a');
    expect(client.groups.single['allowMetered'], isFalse);
    await tester.tap(find.text('Preview route'));
    await tester.pumpAndSettle();
    expect(client.lastPreview, {'model': 'pool:my-work'});
    expect(find.text('Would use provider/model'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
