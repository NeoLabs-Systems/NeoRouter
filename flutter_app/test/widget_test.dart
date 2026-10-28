import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_auth.dart';
import 'package:neorouter/main_controller.dart';
import 'package:neorouter/main_theme.dart';

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  Widget wrap(Widget child, Locale locale) => MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        locale: locale,
        supportedLocales: AppL10n.supportedLocales,
        localizationsDelegates: const <LocalizationsDelegate<Object>>[
          AppL10n.delegate,
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        home: child,
      );

  testWidgets('bootstrap screen invites the first admin to register', (tester) async {
    final controller = NeoRouterController()..bootstrapRegistration = true;
    await tester.pumpWidget(wrap(NeoRouterAuthScreen(controller: controller), const Locale('de')));
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('NeoRouter einrichten'), findsOneWidget);
    expect(find.text('Konto erstellen'), findsOneWidget);
  });

  testWidgets('login screen renders in English', (tester) async {
    final controller = NeoRouterController();
    await tester.pumpWidget(wrap(NeoRouterAuthScreen(controller: controller), const Locale('en')));
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('Welcome back'), findsOneWidget);
    expect(find.text('Sign in'), findsOneWidget);
  });
}
