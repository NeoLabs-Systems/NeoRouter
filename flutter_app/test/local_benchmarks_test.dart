import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_key_expiry.dart';
import 'package:neorouter/main_keys.dart';
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

class BenchmarkKeyClient extends ConsentClient {
  BenchmarkKeyClient() : super(true);

  final key = <String, dynamic>{
    'id': 'personal-key',
    'name': 'Personal key',
    'prefix': 'nrt_fixture',
    'mode': 'free_only',
    'poolAccess': 'pool',
    'benchmarkEligible': true,
    'benchmarkCollection': false,
  };

  @override
  Future<Map<String, dynamic>> get(String path) async => switch (path) {
    '/api/v1/keys' => {
      'keys': [Map<String, dynamic>.of(key)],
    },
    '/api/v1/routing/profiles' => {'profiles': []},
    '/api/v1/routing/groups' => {'groups': []},
    '/api/v1/models/capabilities' => {'capabilities': []},
    _ => await super.get(path),
  };

  @override
  Future<Map<String, dynamic>> patch(String path, Object body) async {
    key.addAll(Map<String, dynamic>.from(body as Map));
    return {'key': Map<String, dynamic>.of(key)};
  }
}

Widget wrap(Widget child, {bool scroll = true}) => MaterialApp(
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
    body: scroll
        ? SingleChildScrollView(
            child: Padding(padding: const EdgeInsets.all(16), child: child),
          )
        : child,
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
      expect(
        find.ancestor(
          of: find.byType(Switch),
          matching: find.byWidgetPredicate(
            (widget) =>
                widget is Semantics &&
                widget.properties.label ==
                    'Allow private collection and replay to my free providers' &&
                widget.properties.hint?.contains('up to 24 hours') == true,
          ),
        ),
        findsOneWidget,
      );
      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();
      expect(client.submitted?['enabled'], true);
      expect(
        find.textContaining('eligible free inference provider'),
        findsOneWidget,
      );
      expect(find.textContaining('memory for up to 24 hours'), findsOneWidget);
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
      expect(
        find.ancestor(
          of: find.byType(Switch),
          matching: find.bySemanticsLabel('Enable local benchmarking'),
        ),
        findsOneWidget,
      );
      await tester.pumpWidget(
        wrap(
          LocalBenchmarkAdmin(
            health: const {
              'enabled': true,
              'lastCode': 'waiting_free_models',
              'counts': {'pending': 2, 'validated': 4},
              'aggregates': 0,
            },
            onChanged: (_) {},
            onRefresh: () {},
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Background evaluator'), findsOneWidget);
      expect(find.textContaining('4 validated model scores'), findsOneWidget);
      expect(find.text('Waiting for eligible free models'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
  testWidgets('the key editor explains consent and keeps pool access enabled', (
    tester,
  ) async {
    final client = BenchmarkKeyClient();
    final controller = NeoRouterController(client: client);
    await tester.pumpWidget(
      wrap(KeysScreen(controller: controller), scroll: false),
    );
    await tester.pumpAndSettle();

    Future<void> edit() async {
      await tester.tap(find.byTooltip('More actions'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Edit'));
      await tester.pumpAndSettle();
    }

    final switchLabel = find.text(
      'Collect completed chats for local evaluation',
    );
    await edit();
    expect(find.textContaining('Settings → Account'), findsOneWidget);
    expect(switchLabel, findsNothing);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    client.enabled = true;
    await edit();
    expect(
      tester.getBottomLeft(find.byType(KeyExpiryField)).dy,
      lessThan(tester.getTopLeft(find.text('LOCAL EVALUATIONS')).dy),
    );
    await tester.ensureVisible(switchLabel);
    await tester.tap(switchLabel);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(client.key['poolAccess'], 'pool');
    expect(client.key['benchmarkCollection'], true);

    await edit();
    expect(switchLabel, findsOneWidget);
    await tester.tap(switchLabel);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(client.key['benchmarkCollection'], false);
  });
}
