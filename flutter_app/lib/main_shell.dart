import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_activity.dart';
import 'main_admin.dart';
import 'main_controller.dart';
import 'main_dashboard.dart';
import 'main_keys.dart';
import 'main_models.dart';
import 'main_playground.dart';
import 'main_providers.dart';
import 'main_routing.dart';
import 'main_settings.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

class NavItem {
  const NavItem(this.page, this.label, this.icon);

  final AppPage page;
  final String label;
  final IconData icon;
}

/// One flat list: what you use every day first, setup next.
List<NavItem> navigationItems(AppL10n t) => <NavItem>[
      NavItem(AppPage.home, t.navHome, Icons.home_outlined),
      NavItem(AppPage.playground, t.navPlayground, Icons.chat_bubble_outline_rounded),
      NavItem(AppPage.activity, t.navActivity, Icons.show_chart_rounded),
      NavItem(AppPage.models, t.navModels, Icons.auto_awesome_outlined),
      NavItem(AppPage.providers, t.navProviders, Icons.link_rounded),
      NavItem(AppPage.keys, t.navKeys, Icons.key_outlined),
      NavItem(AppPage.routing, t.navRouting, Icons.tune_rounded),
    ];

class NeoRouterShell extends StatelessWidget {
  const NeoRouterShell({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final wide = MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet;
    final sidebar = _Sidebar(controller: controller);
    return AmbientBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: wide
            ? null
            : AppBar(
                backgroundColor: palette.scaffold,
                surfaceTintColor: Colors.transparent,
                scrolledUnderElevation: 0,
                titleSpacing: 0,
                title: const BrandLockup(logoSize: 24),
                shape: Border(bottom: BorderSide(color: palette.border)),
              ),
        drawer: wide ? null : Drawer(backgroundColor: palette.surface, width: 280, shape: const RoundedRectangleBorder(), child: sidebar),
        body: Row(
          children: <Widget>[
            if (wide) sidebar,
            Expanded(
              child: AnimatedSwitcher(
                duration: const Duration(milliseconds: 160),
                switchInCurve: Curves.easeOut,
                switchOutCurve: Curves.easeIn,
                transitionBuilder: (child, animation) => FadeTransition(opacity: animation, child: child),
                layoutBuilder: (current, previous) => Stack(children: <Widget>[if (current != null) current]),
                child: KeyedSubtree(key: ValueKey<AppPage>(controller.page), child: _page(controller.page)),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _page(AppPage page) => switch (page) {
        AppPage.home => DashboardScreen(controller: controller),
        AppPage.playground => PlaygroundScreen(controller: controller),
        AppPage.activity => ActivityScreen(controller: controller),
        AppPage.models => ModelsScreen(controller: controller),
        AppPage.providers => ProvidersScreen(controller: controller),
        AppPage.keys => KeysScreen(controller: controller),
        AppPage.routing => RoutingScreen(controller: controller),
        AppPage.settings => SettingsScreen(controller: controller),
        AppPage.admin => controller.user?.isAdmin == true ? AdminScreen(controller: controller) : DashboardScreen(controller: controller),
      };
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.controller});

  final NeoRouterController controller;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final t = AppL10n.of(context);
    void go(AppPage page) {
      controller.go(page);
      if (Scaffold.maybeOf(context)?.isDrawerOpen == true) Navigator.of(context).pop();
    }

    return Container(
      width: 240,
      decoration: BoxDecoration(color: palette.scaffold, border: Border(right: BorderSide(color: palette.border))),
      child: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            const Padding(padding: EdgeInsets.fromLTRB(20, 22, 16, 22), child: Align(alignment: Alignment.centerLeft, child: BrandLockup(logoSize: 26))),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                children: <Widget>[
                  for (final item in navigationItems(t))
                    _SidebarButton(label: item.label, icon: item.icon, active: controller.page == item.page, onTap: () => go(item.page)),
                  if (controller.user?.isAdmin == true) ...<Widget>[
                    Padding(padding: const EdgeInsets.fromLTRB(12, 18, 12, 6), child: Text(t.navSectionServer.toUpperCase(), style: sectionEyebrowStyle(palette))),
                    _SidebarButton(label: t.navAdmin, icon: Icons.dns_outlined, active: controller.page == AppPage.admin, onTap: () => go(AppPage.admin)),
                  ],
                ],
              ),
            ),
            Divider(height: 1, color: palette.border),
            Padding(
              padding: const EdgeInsets.all(12),
              child: _AccountButton(
                name: controller.user?.displayName ?? controller.user?.username ?? t.account,
                email: controller.user?.email ?? '',
                active: controller.page == AppPage.settings,
                onTap: () => go(AppPage.settings),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _AccountButton extends StatelessWidget {
  const _AccountButton({required this.name, required this.email, required this.active, required this.onTap});

  final String name;
  final String email;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final initial = name.trim().isEmpty ? 'N' : name.trim().characters.first.toUpperCase();
    return Material(
      color: active ? palette.surfaceRaised : Colors.transparent,
      borderRadius: BorderRadius.circular(AppRadius.control),
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.control),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(8),
          child: Row(
            children: <Widget>[
              CircleAvatar(
                radius: 15,
                backgroundColor: palette.surfaceRaised,
                child: Text(initial, style: TextStyle(color: palette.text, fontWeight: FontWeight.w600, fontSize: 12.5)),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.text, fontSize: 13, fontWeight: FontWeight.w600)),
                    if (email.isNotEmpty)
                      Text(email, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textMuted, fontSize: 12)),
                  ],
                ),
              ),
              Icon(Icons.settings_outlined, size: 17, color: palette.textMuted),
            ],
          ),
        ),
      ),
    );
  }
}

class _SidebarButton extends StatelessWidget {
  const _SidebarButton({required this.label, required this.icon, required this.onTap, this.active = false});

  final String label;
  final IconData icon;
  final VoidCallback onTap;
  final bool active;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final radius = BorderRadius.circular(AppRadius.control);
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: Material(
        color: active ? palette.surfaceRaised : Colors.transparent,
        borderRadius: radius,
        child: InkWell(
          borderRadius: radius,
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
            child: Row(
              children: <Widget>[
                Icon(icon, size: 18, color: active ? palette.accent : palette.textMuted),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13.5, fontWeight: active ? FontWeight.w600 : FontWeight.w500, color: active ? palette.text : palette.textSoft),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
