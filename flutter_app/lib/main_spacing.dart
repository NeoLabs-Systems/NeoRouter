import 'package:flutter/widgets.dart';

class AppSpacing {
  const AppSpacing._();

  static const double xs = 8;
  static const double sm = 12;
  static const double md = 16;
  static const double lg = 24;
  static const double xl = 32;
}

class AppRadius {
  const AppRadius._();

  static const double tag = 8;
  static const double control = 10;
  static const double card = 14;
  static const double input = 10;
  static const double panel = 18;
  static const double pill = 999;
}

class AppBreakpoints {
  const AppBreakpoints._();

  static const double mobile = 640;
  static const double tablet = 980;
  static const double desktop = 1320;
}

/// Widest the content of a page grows; wider windows center it.
const double kContentMaxWidth = 1120;

EdgeInsets pagePadding(BuildContext context) {
  final width = MediaQuery.sizeOf(context).width;
  if (width >= 1280) return const EdgeInsets.fromLTRB(48, 40, 48, 48);
  if (width >= 900) return const EdgeInsets.fromLTRB(32, 32, 32, 40);
  return const EdgeInsets.fromLTRB(16, 16, 16, 32);
}
