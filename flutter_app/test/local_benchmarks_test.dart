import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_local_benchmarks.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/backend_client.dart';

class ConsentClient extends BackendClient {
  ConsentClient(this.adminEnabled);
  final bool adminEnabled;
  bool enabled = false;
  Map<String, dynamic>? submitted;
  Map<String, dynamic> status() => {
    'adminEnabled': adminEnabled,
    'enabled': enabled,
    'allowReplay': enabled,
    'retentionDays': 7,
    'counts': {},
    'contributions': 0,
  };
  @override
  Future<Map<String, dynamic>> get(String path) async => status();
  @override
  Future<Map<String, dynamic>> putJson(String path, Object body) async {
    submitted = Map<String, dynamic>.from(body as Map);
    enabled = submitted!['enabled'] == true;
    return status();
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
  home: Scaffold(
    body: SingleChildScrollView(
      child: Padding(padding: const EdgeInsets.all(16), child: child),
    ),
  ),
);
void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);
  testWidgets(
    'parent-disabled account controls stay hidden and consent explicitly covers replay',
    (tester) async {
      final disabled = ConsentClient(false);
      await tester.pumpWidget(
        wrap(
          LocalBenchmarkSettings(
            controller: NeoRouterController(client: disabled),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.byType(Switch), findsNothing);
      expect(find.byType(DropdownButton<int>), findsNothing);
      final client = ConsentClient(true);
      await tester.pumpWidget(
        wrap(
          LocalBenchmarkSettings(
            key: UniqueKey(),
            controller: NeoRouterController(client: client),
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(tester.widget<Switch>(find.byType(Switch)).value, false);
      expect(find.byType(DropdownButton<int>), findsNothing);
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(client.submitted?['enabled'], true);
      expect(client.submitted?['allowReplay'], true);
      expect(find.byType(DropdownButton<int>), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets(
    'admin health reveals no child controls until enabled and fits a narrow screen',
    (tester) async {
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        wrap(
          LocalBenchmarkAdmin(
            health: const {'enabled': false},
            onChanged: (_) {},
            onRefresh: () {},
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Background evaluator'), findsNothing);
      await tester.pumpWidget(
        wrap(
          LocalBenchmarkAdmin(
            health: const {
              'enabled': true,
              'lastCode': 'waiting_free_models',
              'counts': {'pending': 2},
              'aggregates': 0,
            },
            onChanged: (_) {},
            onRefresh: () {},
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Background evaluator'), findsOneWidget);
      expect(
        find.text('Waiting for four distinct eligible free models'),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    },
  );
}
