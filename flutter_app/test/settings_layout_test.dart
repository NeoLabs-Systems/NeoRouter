@TestOn('browser')
library;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoSettingsLayout, NeoSidebarButton;
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_settings.dart';
import 'package:neorouter/main_shell.dart';
import 'package:neorouter/main_theme.dart';
import 'package:neorouter/src/backend_client.dart';

class _SettingsClient extends BackendClient {
  @override
  Future<Map<String, dynamic>> get(String path) async => switch (path) {
        '/api/v1/auth/2fa' => <String, dynamic>{'enabled': false},
        _ => throw StateError(path),
      };
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  final dark = neoRouterPaletteFor(Brightness.dark);

  Future<void> pumpSettings(WidgetTester tester, {required Size size}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        locale: const Locale('en'),
        localizationsDelegates: AppL10n.localizationsDelegates,
        supportedLocales: AppL10n.supportedLocales,
        home: Scaffold(body: SettingsScreen(controller: NeoRouterController(client: _SettingsClient()))),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('a wide screen puts the library rail beside the open page', (tester) async {
    await pumpSettings(tester, size: const Size(1200, 900));
    expect(find.byType(NeoSettingsLayout), findsOneWidget);
    // Nav list plus the open page, side by side.
    expect(find.byType(ListView), findsNWidgets(2));
    final rail = tester.widget<Container>(
      find.descendant(of: find.byType(NeoSettingsLayout), matching: find.byType(Container)).first,
    );
    expect(rail.constraints, BoxConstraints.tightFor(width: 268));
    final decoration = rail.decoration! as BoxDecoration;
    expect(decoration.color, dark.surface);
    expect(decoration.border, Border(right: BorderSide(color: dark.border)));

    // Choosing another page keeps the rail: it opens beside it.
    final strings = await AppL10n.delegate.load(const Locale('en'));
    await tester.tap(find.text(strings.account).last);
    await tester.pumpAndSettle();
    expect(find.byType(ListView), findsNWidgets(2));
    expect(find.byIcon(Icons.arrow_back_ios_new_rounded), findsNothing);
  });

  testWidgets('a phone shows the list alone, then the page behind a back link', (tester) async {
    await pumpSettings(tester, size: const Size(390, 750));
    expect(find.byType(NeoSettingsLayout), findsOneWidget);
    expect(find.byType(ListView), findsOneWidget);
    final popScope = find.byWidgetPredicate((widget) => widget is PopScope);
    expect(popScope, findsNothing);

    final strings = await AppL10n.delegate.load(const Locale('en'));
    await tester.tap(find.text(strings.account).last);
    await tester.pumpAndSettle();
    // The page covers the list, and the browser's back gesture returns to it.
    expect(find.byType(ListView), findsOneWidget);
    expect(popScope, findsOneWidget);
    expect((tester.widget(popScope) as PopScope).canPop, isFalse);
    final back = find.byIcon(Icons.arrow_back_ios_new_rounded);
    expect(back, findsOneWidget);
    await tester.tap(back);
    await tester.pumpAndSettle();
    expect(find.byIcon(Icons.arrow_back_ios_new_rounded), findsNothing);
    expect(find.text(strings.settingsSearchHint), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the shell draws its navigation with the library rows', (tester) async {
    tester.view.physicalSize = const Size(1200, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final controller = NeoRouterController(client: _SettingsClient())..page = AppPage.settings;
    await tester.pumpWidget(
      MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        locale: const Locale('en'),
        localizationsDelegates: AppL10n.localizationsDelegates,
        supportedLocales: AppL10n.supportedLocales,
        home: NeoRouterShell(controller: controller),
      ),
    );
    await tester.pump();
    // One library row per navigation group, plus the open group's sub-rows.
    expect(find.byType(NeoSidebarButton), findsWidgets);
    final groupRow = tester.element(find.byType(NeoSidebarButton).first);
    final strings = AppL10n.of(groupRow);
    expect(find.text(strings.groupOverview), findsOneWidget);
    expect(find.byType(NeoSettingsLayout), findsOneWidget);
    // Leave nothing ticking behind (the mascot polls).
    await tester.pumpWidget(const SizedBox.shrink());
    expect(tester.takeException(), isNull);
  });
}