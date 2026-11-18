import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neorouter/l10n/gen/app_localizations.dart';
import 'package:neorouter/main_key_expiry.dart';
import 'package:neorouter/main_theme.dart';

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
    body: Padding(padding: const EdgeInsets.all(16), child: child),
  ),
);

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  testWidgets(
    'duration selection sets a deadline and cancelling custom selection preserves it',
    (tester) async {
      DateTime? value;
      await tester.pumpWidget(
        wrap(
          StatefulBuilder(
            builder: (context, setState) => KeyExpiryField(
              value: value,
              onChanged: (next) => setState(() => value = next),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byType(DropdownButtonFormField<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('30 days from now').last);
      await tester.pumpAndSettle();
      expect(value, isNotNull);
      expect(
        value!.difference(DateTime.now()).inHours,
        inInclusiveRange(719, 720),
      );
      expect(find.textContaining('UTC'), findsOneWidget);
      final original = value;
      await tester.tap(find.byType(DropdownButtonFormField<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Custom date and time').last);
      await tester.pumpAndSettle();
      expect(find.byType(DatePickerDialog), findsOneWidget);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(value, original);
      expect(
        tester
            .widget<DropdownButtonFormField<String>>(
              find.byType(DropdownButtonFormField<String>),
            )
            .initialValue,
        '30',
      );
    },
  );

  testWidgets(
    'expired dates remain unchanged on opening and are readable on a small screen',
    (tester) async {
      await tester.binding.setSurfaceSize(const Size(360, 760));
      addTearDown(() => tester.binding.setSurfaceSize(null));
      final original = DateTime.now().subtract(const Duration(days: 1));
      var changes = 0;
      await tester.pumpWidget(
        wrap(
          KeyExpiryField(
            value: original,
            wasExpired: true,
            onChanged: (_) => changes++,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(changes, 0);
      expect(find.text('Expired'), findsOneWidget);
      expect(find.textContaining('Its secret stays the same.'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}
