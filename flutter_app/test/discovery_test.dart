import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/discovery/benchmarks_page.dart';
import 'package:neorouter/discovery/widgets.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_shared.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/backend_client.dart';

class DiscoveryClient extends BackendClient {
  final requests = <Uri>[];
  bool showLocalEvidence = false;
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
      return {
        'enabled': showLocalEvidence,
        'results': showLocalEvidence
            ? [
                for (final task in ['coding', 'math'])
                  {
                    'model': 'example-local-model',
                    'name': 'Example Local Model',
                    'task': task,
                    'samples': 8,
                    'confidence': 0.85,
                    'score': task == 'coding' ? 0.75 : 0.25,
                    'evaluatedAt': 1794954000000,
                    'evaluators': [
                      {
                        'method': 'constraints',
                        'version': 'neorouter-constraints/1',
                        'samples': 2,
                        'score': 1,
                      },
                      {
                        'method': 'judge',
                        'version': 'neorouter-blind-pairwise/1',
                        'samples': 6,
                        'score': 0.65,
                      },
                    ],
                  },
              ]
            : [],
      };
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
  testWidgets(
    'local preference chart groups tasks and opens evaluation details',
    (tester) async {
      final client = DiscoveryClient()..showLocalEvidence = true;
      final controller = NeoRouterController(client: client);
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(wrap(BenchmarksScreen(controller: controller)));
      await tester.pumpAndSettle();
      final section = find.text('ReplayBench');
      await tester.ensureVisible(section);
      expect(
        find.text('See how models compare on sanitized, opt-in traffic.'),
        findsOneWidget,
      );
      expect(find.text('Example Local Model'), findsOneWidget);
      expect(find.text('Coding · 8 comparisons'), findsOneWidget);
      expect(find.text('Math · 8 comparisons'), findsOneWidget);
      expect(
        tester
            .widgetList<BenchmarkScoreBar>(
              find.descendant(
                of: find.ancestor(
                  of: section,
                  matching: find.byType(SectionCard),
                ),
                matching: find.byType(BenchmarkScoreBar),
              ),
            )
            .map((bar) => bar.score),
        [75, 25],
      );
      expect(find.text('Scoring breakdown'), findsNothing);
      expect(find.textContaining('confidence 85%'), findsNothing);
      expect(find.text('First model'), findsWidgets);
      final details = find.text('Coding · 8 comparisons');
      await tester.ensureVisible(details);
      await tester.pumpAndSettle();
      await tester.tap(details);
      await tester.pumpAndSettle();
      expect(find.text('Scoring breakdown'), findsOneWidget);
      expect(find.text('Response checks'), findsOneWidget);
      expect(find.text('Judge 1'), findsOneWidget);
      expect(
        find.text('6 comparisons · neorouter-blind-pairwise/1'),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    },
  );
}
