import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoLabsPalette, NeoSidebarButton;
import 'package:neorouter/main_theme.dart';

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  final dark = neoRouterPaletteFor(Brightness.dark);

  Widget wrap(Widget child) => MaterialApp(
        theme: buildNeoRouterTheme(Brightness.dark),
        home: Scaffold(body: child),
      );

  Color? rowColor(WidgetTester tester) =>
      (tester.widget<AnimatedContainer>(find.byType(AnimatedContainer)).decoration as BoxDecoration).color;

  test('the library palette carries NeoRouter tokens', () {
    final library = neoLabsPaletteFor(Brightness.dark);
    expect(library, isA<NeoLabsPalette>());
    // The tokens a sidebar row reads.
    expect(library.accent, dark.accent);
    expect(library.accentMuted, dark.accentSoft);
    expect(library.bgTertiary, dark.surfaceRaised);
    expect(library.textPrimary, dark.text);
    expect(library.textSecondary, dark.textSoft);
    expect(library.textMuted, dark.textMuted);
    final light = neoLabsPaletteFor(Brightness.light);
    expect(light.accentMuted, neoRouterPaletteFor(Brightness.light).accentSoft);
  });

  testWidgets('an active row is one accent wash with a gold glyph', (tester) async {
    await tester.pumpWidget(wrap(NeoSidebarButton(label: 'Dashboard', icon: Icons.space_dashboard_outlined, iconSize: 18, active: true, palette: neoLabsPaletteFor(Brightness.dark), onTap: () {})));
    expect(rowColor(tester), dark.accentSoft);
    final icon = tester.widget<Icon>(find.byIcon(Icons.space_dashboard_outlined));
    expect(icon.size, 18);
    expect(icon.color, dark.accent);
    final label = tester.widget<Text>(find.text('Dashboard'));
    expect(label.style?.fontSize, 13);
    expect(label.style?.color, dark.text);
  });

  testWidgets('a resting row is untinted and muted', (tester) async {
    await tester.pumpWidget(wrap(NeoSidebarButton(label: 'Models', icon: Icons.view_list_outlined, palette: neoLabsPaletteFor(Brightness.dark), onTap: () {})));
    expect(rowColor(tester), Colors.transparent);
    final icon = tester.widget<Icon>(find.byIcon(Icons.view_list_outlined));
    expect(icon.size, 18);
    expect(icon.color, dark.textMuted);
    expect(tester.widget<Text>(find.text('Models')).style?.color, dark.textSoft);
  });

  testWidgets('a compact sub-row draws a 16px glyph in a 12px radius', (tester) async {
    await tester.pumpWidget(wrap(NeoSidebarButton(label: 'Activity', icon: Icons.insights_outlined, compact: true, iconSize: 16, palette: neoLabsPaletteFor(Brightness.dark), onTap: () {})));
    expect(tester.widget<Icon>(find.byIcon(Icons.insights_outlined)).size, 16);
    final box = tester.widget<AnimatedContainer>(find.byType(AnimatedContainer));
    expect((box.decoration as BoxDecoration).borderRadius, BorderRadius.circular(12));
  });
}