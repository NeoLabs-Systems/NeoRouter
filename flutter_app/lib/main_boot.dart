import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import 'main_theme.dart';

/// In-app boot screen shown while the session is restored. It mirrors the
/// HTML splash in web/index.html so the hand-over to Flutter is seamless.
class BootScreen extends StatefulWidget {
  const BootScreen({super.key});

  @override
  State<BootScreen> createState() => _BootScreenState();
}

class _BootScreenState extends State<BootScreen> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(vsync: this, duration: const Duration(milliseconds: 2400))..repeat();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final dark = Theme.of(context).brightness == Brightness.dark;
    return Scaffold(
      body: DecoratedBox(
        decoration: BoxDecoration(
          gradient: RadialGradient(
            center: const Alignment(0, -0.24),
            radius: 1.2,
            colors: dark
                ? const <Color>[Color(0xFF131B16), Color(0xFF0A0F0C), Color(0xFF0E1511)]
                : const <Color>[Color(0xFFFDFCF8), Color(0xFFEDE9DC), Color(0xFFF4F1E8)],
            stops: const <double>[0, 0.7, 1],
          ),
        ),
        child: Center(
          child: AnimatedBuilder(
            animation: _controller,
            builder: (context, _) {
              final phase = _controller.value;
              return Column(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Transform.scale(
                    scale: 1 + 0.045 * math.sin(phase * 2 * math.pi).abs(),
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        borderRadius: BorderRadius.circular(20),
                        boxShadow: <BoxShadow>[BoxShadow(color: Colors.black.withValues(alpha: dark ? 0.35 : 0.18), blurRadius: 18, offset: const Offset(0, 6))],
                      ),
                      child: SvgPicture.asset('assets/branding/logo.svg', width: 88, height: 88),
                    ),
                  ),
                  const SizedBox(height: 20),
                  Text(
                    'NEOROUTER',
                    style: TextStyle(color: palette.textSoft, fontSize: 15, fontWeight: FontWeight.w600, letterSpacing: 4.2),
                  ),
                  const SizedBox(height: 20),
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      for (var index = 0; index < 3; index++)
                        Builder(builder: (context) {
                          final local = ((phase * 2 - index * 0.125) % 1 + 1) % 1;
                          final lift = local < 0.4 ? math.sin(local / 0.4 * math.pi) : 0.0;
                          return Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 3),
                            child: Transform.translate(
                              offset: Offset(0, -3 * lift),
                              child: Container(
                                width: 5,
                                height: 5,
                                decoration: BoxDecoration(color: palette.accent.withValues(alpha: 0.35 + 0.65 * lift), shape: BoxShape.circle),
                              ),
                            ),
                          );
                        }),
                    ],
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}
