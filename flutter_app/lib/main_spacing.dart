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

  static const double tag = 10;
  static const double card = 16;
  static const double input = 16;
  static const double panel = 24;
  static const double pill = 999;
}

class AppBreakpoints {
  const AppBreakpoints._();

  static const double mobile = 640;
  static const double tablet = 980;
  static const double desktop = 1320;
}

EdgeInsets pagePadding(BuildContext context) {
  final width = MediaQuery.sizeOf(context).width;
  if (width >= 1280) {
    return const EdgeInsets.fromLTRB(40, 34, 40, 40);
  }
  if (width >= 900) {
    return const EdgeInsets.fromLTRB(30, 28, 30, 32);
  }
  return const EdgeInsets.fromLTRB(20, 20, 20, 28);
}
