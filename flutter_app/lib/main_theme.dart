import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'main_spacing.dart';

/// Color tokens. One accent (NeoLabs gold) marks primary actions and the
/// current selection; everything else is neutral olive-grey. Status colors
/// only ever appear as small dots or short status words, never as fills.
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

  /// Neutral second series color for charts (no second hue).
  final Color secondary;
  final Color success;
  final Color warning;
  final Color error;

  /// Kept for compatibility; neutral like [secondary].
  final Color info;
  final Color shadow;
  final Color glassFill;
  final Color glassBorder;
  final Gradient backgroundGradient;
  final Gradient panelGradient;
}

// NeoAgent's tokens (deep olive by night, paper by day, NeoLabs gold), with
// the extra hues removed.
const NeoRouterPalette _darkPalette = NeoRouterPalette(
  scaffold: Color(0xFF0E1511),
  surface: Color(0xFF141C17),
  surfaceRaised: Color(0xFF1B241E),
  surfaceMuted: Color(0xFF19211C),
  border: Color(0x14E0F0E0),
  borderStrong: Color(0x22E0F0E0),
  text: Color(0xFFECEFE5),
  textSoft: Color(0xFFA9B2A2),
  textMuted: Color(0xFF768070),
  accent: Color(0xFFE1B052),
  accentStrong: Color(0xFFEAC27A),
  accentSoft: Color(0x1FE1B052),
  onAccent: Color(0xFF14110A),
  secondary: Color(0xFF6E7A6A),
  success: Color(0xFF7DBF86),
  warning: Color(0xFFD7A84E),
  error: Color(0xFFE08A79),
  info: Color(0xFF6E7A6A),
  shadow: Color(0x33000000),
  glassFill: Color(0xFF141C17),
  glassBorder: Color(0x22E0F0E0),
  backgroundGradient: LinearGradient(colors: <Color>[Color(0xFF0E1511), Color(0xFF0E1511)]),
  panelGradient: LinearGradient(colors: <Color>[Color(0xFF141C17), Color(0xFF141C17)]),
);

const NeoRouterPalette _lightPalette = NeoRouterPalette(
  scaffold: Color(0xFFF6F4EE),
  surface: Color(0xFFFFFFFF),
  surfaceRaised: Color(0xFFF1EEE6),
  surfaceMuted: Color(0xFFF3F1EA),
  border: Color(0x141C2117),
  borderStrong: Color(0x241C2117),
  text: Color(0xFF1C2117),
  textSoft: Color(0xFF545B4B),
  textMuted: Color(0xFF858B7B),
  accent: Color(0xFFB07D2B),
  accentStrong: Color(0xFF8A5F1C),
  accentSoft: Color(0x1AB07D2B),
  onAccent: Color(0xFFFFFFFF),
  secondary: Color(0xFFA6AB9C),
  success: Color(0xFF4F8A55),
  warning: Color(0xFFA9741C),
  error: Color(0xFFB4483C),
  info: Color(0xFFA6AB9C),
  shadow: Color(0x141C2117),
  glassFill: Color(0xFFFFFFFF),
  glassBorder: Color(0x241C2117),
  backgroundGradient: LinearGradient(colors: <Color>[Color(0xFFF6F4EE), Color(0xFFF6F4EE)]),
  panelGradient: LinearGradient(colors: <Color>[Color(0xFFFFFFFF), Color(0xFFFFFFFF)]),
);

NeoRouterPalette neoRouterPaletteFor(Brightness brightness) =>
    brightness == Brightness.dark ? _darkPalette : _lightPalette;

NeoRouterPalette neoRouterPaletteOf(BuildContext context) =>
    neoRouterPaletteFor(Theme.of(context).brightness);

/// Small label over a group of fields: muted, not colored.
TextStyle sectionEyebrowStyle(NeoRouterPalette palette) => TextStyle(
  color: palette.textMuted,
  fontWeight: FontWeight.w600,
  fontSize: 11.5,
  letterSpacing: 0.8,
);

TextStyle displayTitleStyle(NeoRouterPalette palette, {double size = 28}) =>
    TextStyle(
      color: palette.text,
      fontWeight: FontWeight.w700,
      fontSize: size,
      letterSpacing: size >= 24 ? -0.7 : -0.3,
      height: 1.15,
    );

/// Title of a card or section.
TextStyle sectionTitleStyle(NeoRouterPalette palette) =>
    TextStyle(color: palette.text, fontWeight: FontWeight.w600, fontSize: 15, letterSpacing: -0.15);

ThemeData buildNeoRouterTheme(Brightness brightness) {
  final palette = neoRouterPaletteFor(brightness);
  final base = ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: ColorScheme.fromSeed(seedColor: palette.accent, brightness: brightness),
  );
  final buttonShape = RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control));
  final inputBorder = OutlineInputBorder(
    borderRadius: BorderRadius.circular(AppRadius.control),
    borderSide: BorderSide(color: palette.borderStrong),
  );

  return base.copyWith(
    scaffoldBackgroundColor: palette.scaffold,
    colorScheme: base.colorScheme.copyWith(
      primary: palette.accent,
      onPrimary: palette.onAccent,
      secondary: palette.textSoft,
      tertiary: palette.textSoft,
      surface: palette.surface,
      onSurface: palette.text,
      surfaceContainerHighest: palette.surfaceRaised,
      outline: palette.borderStrong,
      outlineVariant: palette.border,
      error: palette.error,
    ),
    textTheme: GoogleFonts.geistTextTheme(base.textTheme).apply(bodyColor: palette.text, displayColor: palette.text),
    splashFactory: InkRipple.splashFactory,
    hoverColor: palette.text.withValues(alpha: 0.04),
    highlightColor: Colors.transparent,
    cardTheme: CardThemeData(
      color: palette.surface,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.card),
        side: BorderSide(color: palette.border),
      ),
    ),
    dividerColor: palette.border,
    dividerTheme: DividerThemeData(color: palette.border, thickness: 1, space: 1),
    dialogTheme: DialogThemeData(
      backgroundColor: palette.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.panel),
        side: BorderSide(color: palette.border),
      ),
      titleTextStyle: GoogleFonts.geist(color: palette.text, fontSize: 18, fontWeight: FontWeight.w600),
    ),
    bottomSheetTheme: BottomSheetThemeData(backgroundColor: palette.surface, surfaceTintColor: Colors.transparent),
    popupMenuTheme: PopupMenuThemeData(
      color: palette.surface,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control), side: BorderSide(color: palette.border)),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: palette.text,
      contentTextStyle: TextStyle(color: palette.scaffold, fontSize: 13.5),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control)),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: palette.surface,
      isDense: true,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      labelStyle: TextStyle(color: palette.textMuted, fontSize: 13.5),
      floatingLabelStyle: TextStyle(color: palette.textSoft, fontSize: 13),
      hintStyle: TextStyle(color: palette.textMuted),
      helperStyle: TextStyle(color: palette.textMuted, fontSize: 12),
      border: inputBorder,
      enabledBorder: inputBorder,
      focusedBorder: inputBorder.copyWith(borderSide: BorderSide(color: palette.accent, width: 1.2)),
      errorBorder: inputBorder.copyWith(borderSide: BorderSide(color: palette.error)),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: palette.accent,
        foregroundColor: palette.onAccent,
        disabledBackgroundColor: palette.surfaceRaised,
        disabledForegroundColor: palette.textMuted,
        elevation: 0,
        minimumSize: const Size(0, 40),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: palette.text,
        side: BorderSide(color: palette.borderStrong),
        minimumSize: const Size(0, 40),
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: palette.textSoft,
        minimumSize: const Size(0, 36),
        shape: buttonShape,
        textStyle: const TextStyle(fontWeight: FontWeight.w500, fontSize: 13.5),
      ),
    ),
    iconButtonTheme: IconButtonThemeData(
      style: IconButton.styleFrom(foregroundColor: palette.textSoft, shape: buttonShape),
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      style: SegmentedButton.styleFrom(
        selectedBackgroundColor: palette.surfaceRaised,
        selectedForegroundColor: palette.text,
        foregroundColor: palette.textSoft,
        side: BorderSide(color: palette.borderStrong),
        visualDensity: VisualDensity.compact,
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
      ),
    ),
    iconTheme: IconThemeData(color: palette.textSoft),
    chipTheme: base.chipTheme.copyWith(
      backgroundColor: palette.surface,
      selectedColor: palette.surfaceRaised,
      checkmarkColor: palette.text,
      side: BorderSide(color: palette.border),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.pill)),
      labelStyle: TextStyle(color: palette.textSoft, fontWeight: FontWeight.w500, fontSize: 13),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      showCheckmark: false,
    ),
    tooltipTheme: TooltipThemeData(
      decoration: BoxDecoration(color: palette.text, borderRadius: BorderRadius.circular(8)),
      textStyle: TextStyle(color: palette.scaffold, fontSize: 12),
      waitDuration: const Duration(milliseconds: 400),
    ),
    // Neutral switches: "on" is the text color, so long lists of toggles stay
    // calm and gold remains reserved for primary actions.
    switchTheme: SwitchThemeData(
      materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
      thumbColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? palette.scaffold : palette.textMuted),
      trackColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? palette.text.withValues(alpha: 0.85) : palette.surfaceRaised),
      trackOutlineColor: WidgetStateProperty.resolveWith((states) => states.contains(WidgetState.selected) ? Colors.transparent : palette.borderStrong),
      thumbIcon: const WidgetStatePropertyAll<Icon?>(null),
    ),
    sliderTheme: base.sliderTheme.copyWith(
      activeTrackColor: palette.accent,
      inactiveTrackColor: palette.surfaceRaised,
      thumbColor: palette.accent,
      overlayColor: palette.accentSoft,
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: palette.accent, linearTrackColor: palette.surfaceRaised),
    tabBarTheme: TabBarThemeData(
      labelColor: palette.text,
      unselectedLabelColor: palette.textMuted,
      indicatorColor: palette.text,
      dividerColor: palette.border,
      labelStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600),
      unselectedLabelStyle: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500),
    ),
  );
}
