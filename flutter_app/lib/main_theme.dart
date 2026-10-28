import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'main_spacing.dart';

class NeoRouterPalette {
  const NeoRouterPalette({
    required this.scaffold,
    required this.surface,
    required this.surfaceRaised,
    required this.surfaceMuted,
    required this.border,
    required this.borderStrong,
    required this.text,
    required this.textSoft,
    required this.textMuted,
    required this.accent,
    required this.accentStrong,
    required this.accentSoft,
    required this.onAccent,
    required this.secondary,
    required this.success,
    required this.warning,
    required this.error,
    required this.info,
    required this.shadow,
    required this.glassFill,
    required this.glassBorder,
    required this.backgroundGradient,
    required this.panelGradient,
  });

  final Color scaffold;
  final Color surface;
  final Color surfaceRaised;
  final Color surfaceMuted;
  final Color border;
  final Color borderStrong;
  final Color text;
  final Color textSoft;
  final Color textMuted;
  final Color accent;
  final Color accentStrong;
  final Color accentSoft;
  final Color onAccent;
  final Color secondary;
  final Color success;
  final Color warning;
  final Color error;
  final Color info;
  final Color shadow;
  final Color glassFill;
  final Color glassBorder;
  final Gradient backgroundGradient;
  final Gradient panelGradient;
}

// Same tokens as NeoAgent (flutter_app/lib/src/theme/palette.dart there):
// deep olive by night, paper by day, NeoLabs gold. Teal is NeoRouter's own
// secondary accent.
const NeoRouterPalette _darkPalette = NeoRouterPalette(
  scaffold: Color(0xFF0E1511),
  surface: Color(0xFF171F1A),
  surfaceRaised: Color(0xFF1C261F),
  surfaceMuted: Color(0xFF1C261F),
  border: Color(0x14E0F0E0),
  borderStrong: Color(0x24E0F0E0),
  text: Color(0xFFECEFE5),
  textSoft: Color(0xFFAEB7A6),
  textMuted: Color(0xFF7E8877),
  accent: Color(0xFFE1B052),
  accentStrong: Color(0xFFEAC272),
  accentSoft: Color(0x24E1B052),
  onAccent: Color(0xFF0E1511),
  secondary: Color(0xFF6FB0A4),
  success: Color(0xFF74C07C),
  warning: Color(0xFFD9A24B),
  error: Color(0xFFDE8A78),
  info: Color(0xFF7A9BB8),
  shadow: Color(0x3D000000),
  glassFill: Color(0xF2171F1A),
  glassBorder: Color(0x24E0F0E0),
  backgroundGradient: LinearGradient(
    colors: <Color>[Color(0xFF0E1511), Color(0xFF0E1511)],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  ),
  panelGradient: LinearGradient(
    colors: <Color>[Color(0xFF171F1A), Color(0xFF171F1A)],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  ),
);

const NeoRouterPalette _lightPalette = NeoRouterPalette(
  scaffold: Color(0xFFF4F1E8),
  surface: Color(0xFFFDFCF8),
  surfaceRaised: Color(0xFFF1EDE1),
  surfaceMuted: Color(0xFFEDE9DC),
  border: Color(0x1A1C2117),
  borderStrong: Color(0x291C2117),
  text: Color(0xFF1C2117),
  textSoft: Color(0xFF49503F),
  textMuted: Color(0xFF6E745F),
  accent: Color(0xFFB07D2B),
  accentStrong: Color(0xFF8A5F1C),
  accentSoft: Color(0x24B07D2B),
  onAccent: Color(0xFFFFFFFF),
  secondary: Color(0xFF2F7D6E),
  success: Color(0xFF527C4F),
  warning: Color(0xFF9A6B1E),
  error: Color(0xFFAE473C),
  info: Color(0xFF3D5A73),
  shadow: Color(0x1F1C2117),
  glassFill: Color(0xF5FDFCF8),
  glassBorder: Color(0x291C2117),
  backgroundGradient: LinearGradient(
    colors: <Color>[Color(0xFFF4F1E8), Color(0xFFF4F1E8)],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  ),
  panelGradient: LinearGradient(
    colors: <Color>[Color(0xFFFDFCF8), Color(0xFFFDFCF8)],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  ),
);

NeoRouterPalette neoRouterPaletteFor(Brightness brightness) =>
    brightness == Brightness.dark ? _darkPalette : _lightPalette;

NeoRouterPalette neoRouterPaletteOf(BuildContext context) =>
    neoRouterPaletteFor(Theme.of(context).brightness);

TextStyle sectionEyebrowStyle(NeoRouterPalette palette) => TextStyle(
  color: palette.accentStrong,
  fontWeight: FontWeight.w700,
  fontSize: 12,
  letterSpacing: 1.6,
);

TextStyle displayTitleStyle(NeoRouterPalette palette, {double size = 28}) =>
    TextStyle(
      color: palette.text,
      fontWeight: FontWeight.w800,
      fontSize: size,
      letterSpacing: -0.9,
      height: 1.1,
    );

ThemeData buildNeoRouterTheme(Brightness brightness) {
  final palette = neoRouterPaletteFor(brightness);
  final base = ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: ColorScheme.fromSeed(
      seedColor: palette.accent,
      brightness: brightness,
    ),
  );

  return base.copyWith(
    scaffoldBackgroundColor: palette.scaffold,
    colorScheme: base.colorScheme.copyWith(
      primary: palette.accent,
      secondary: palette.secondary,
      tertiary: palette.secondary,
      surface: palette.surface,
      onSurface: palette.text,
      error: palette.error,
    ),
    textTheme: GoogleFonts.geistTextTheme(
      base.textTheme,
    ).apply(bodyColor: palette.text, displayColor: palette.text),
    splashFactory: InkSparkle.splashFactory,
    cardTheme: CardThemeData(
      color: palette.surface.withValues(
        alpha: brightness == Brightness.dark ? 0.92 : 0.97,
      ),
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      shadowColor: palette.shadow,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.card),
        side: BorderSide(color: palette.border),
      ),
    ),
    dividerColor: palette.border,
    dialogTheme: DialogThemeData(
      backgroundColor: palette.surfaceRaised,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: palette.surfaceMuted.withValues(
        alpha: brightness == Brightness.dark ? 0.82 : 0.88,
      ),
      contentPadding: const EdgeInsets.symmetric(horizontal: 18, vertical: 18),
      labelStyle: TextStyle(
        color: palette.textMuted,
        fontWeight: FontWeight.w600,
        fontSize: 12,
      ),
      hintStyle: TextStyle(color: palette.textMuted),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: palette.borderStrong),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: palette.borderStrong),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: palette.accent, width: 1.4),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: palette.error, width: 1.2),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: palette.accent,
        foregroundColor: palette.onAccent,
        disabledBackgroundColor: palette.surfaceMuted,
        disabledForegroundColor: palette.textMuted,
        elevation: 0,
        padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 18),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
        textStyle: const TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w700,
          letterSpacing: -0.1,
        ),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: palette.text,
        backgroundColor: palette.surface.withValues(
          alpha: brightness == Brightness.dark ? 0.3 : 0.65,
        ),
        side: BorderSide(color: palette.borderStrong),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 16),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
        textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w700),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: palette.accentStrong,
        textStyle: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13),
      ),
    ),
    iconTheme: IconThemeData(color: palette.textSoft),
    chipTheme: base.chipTheme.copyWith(
      backgroundColor: palette.surfaceMuted,
      selectedColor: palette.accentSoft,
      side: BorderSide(color: palette.borderStrong),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
      labelStyle: TextStyle(
        color: palette.textSoft,
        fontWeight: FontWeight.w600,
        fontSize: 12,
      ),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(
        color: palette.surfaceRaised,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: palette.borderStrong),
      ),
      textStyle: TextStyle(color: palette.text, fontSize: 12),
    ),
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((states) {
        if (states.contains(WidgetState.selected)) return palette.accent;
        return palette.textMuted;
      }),
      trackColor: WidgetStateProperty.resolveWith((states) {
        if (states.contains(WidgetState.selected)) return palette.accentSoft;
        return palette.surfaceMuted;
      }),
    ),
  );
}
