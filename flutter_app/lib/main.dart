import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_auth.dart';
import 'main_boot.dart';
import 'main_connect.dart';
import 'main_controller.dart';
import 'main_shell.dart';
import 'main_theme.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  runApp(const NeoRouterApp());
}

class NeoRouterApp extends StatefulWidget {
  const NeoRouterApp({super.key});

  @override
  State<NeoRouterApp> createState() => _NeoRouterAppState();
}

class _NeoRouterAppState extends State<NeoRouterApp> {
  final NeoRouterController _controller = NeoRouterController();

  @override
  void initState() {
    super.initState();
    _controller.bootstrap();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: _controller,
      builder: (context, _) => MaterialApp(
        title: 'NeoRouter',
        debugShowCheckedModeBanner: false,
        themeMode: _controller.themeMode,
        theme: buildNeoRouterTheme(Brightness.light),
        darkTheme: buildNeoRouterTheme(Brightness.dark),
        locale: _controller.locale,
        supportedLocales: AppL10n.supportedLocales,
        localizationsDelegates: const <LocalizationsDelegate<Object>>[
          AppL10n.delegate,
          GlobalMaterialLocalizations.delegate,
          GlobalWidgetsLocalizations.delegate,
          GlobalCupertinoLocalizations.delegate,
        ],
        home: _controller.bootstrapping
            ? const BootScreen()
            : _controller.hasUser
                ? (_controller.pendingConnect != null ? ConnectAppScreen(controller: _controller) : NeoRouterShell(controller: _controller))
                : NeoRouterAuthScreen(controller: _controller),
      ),
    );
  }
}
