import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/discovery/benchmarks_page.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/backend_client.dart';

class DiscoveryClient extends BackendClient {
  final requests = <Uri>[];
  Map<String, dynamic> model(String id, String name, double score) => {
    'id': id,
    'name': name,
    'capability': 'chat',
    'score': score,
    'sourceCount': 1,
    'isFree': true,
    'scores': [
      {
        'id': 'source:gpqa',
        'sourceId': 'source',
        'metric': 'gpqa',
        'value': score / 100,
        'percentile': score,
        'peers': 2,
      },
    ],
  };
  @override
  Future<Map<String, dynamic>> get(String path) async {
    final uri = Uri.parse(path);
    requests.add(uri);
    if (uri.path.endsWith('/internal-benchmarks')) {
      return {'enabled': false, 'results': []};
    }
    if (uri.path.endsWith('/benchmarks')) {
      return {
        'models': [
          model('first', 'First model', 50),
          model('second', 'Second model', 70),
        ],
        'comparison': [],
        'total': 2,
        'capabilities': ['chat'],
        'sources': [
          {
            'id': 'source',
            'name': 'Measured source',
            'url': 'https://example.test/',
            'updatedAt': '2026-11-17T10:00:00Z',
            'modelCount': 2,
          },
        ],
      };
    }
    throw StateError('Unexpected request: $path');
  }
}

Widget wrap(Widget child) => MaterialApp(
  theme: buildNeoRouterTheme(Brightness.dark),
  locale: const Locale('en'),
  supportedLocales: AppL10n.supportedLocales,
  localizationsDelegates: const [
    AppL10n.delegate,
    GlobalMaterialLocalizations.delegate,
    GlobalWidgetsLocalizations.delegate,
    GlobalCupertinoLocalizations.delegate,
  ],
  home: Scaffold(body: child),
);

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);
  testWidgets(
    'two-model comparison works and the selected view fits a narrow screen',
    (tester) async {
      tester.view.physicalSize = const Size(1280, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final client = DiscoveryClient();
      final controller = NeoRouterController(client: client);
      await tester.pumpWidget(wrap(BenchmarksScreen(controller: controller)));
      await tester.pumpAndSettle();
      final first = find.byTooltip('Compare First model');
      await tester.ensureVisible(first);
      await tester.tap(first);
      await tester.pumpAndSettle();
      final second = find.byTooltip('Compare Second model');
      await tester.ensureVisible(second);
      await tester.tap(second);
      await tester.pumpAndSettle();
      // One paired-bar row per shared test.
      expect(find.text('Measured source · Gpqa'), findsOneWidget);
      expect(
        find.byTooltip('Remove First model from comparison'),
        findsWidgets,
      );
      tester.view.physicalSize = const Size(390, 844);
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
      expect(
        client.requests.where((uri) => uri.path.endsWith('/benchmarks')).length,
        1,
        reason: 'comparison uses already fetched measurements',
      );
    },
  );
}
