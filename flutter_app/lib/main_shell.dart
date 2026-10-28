import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_activity.dart';
import 'main_admin.dart';
import 'main_controller.dart';
import 'main_dashboard.dart';
import 'main_keys.dart';
import 'main_models.dart';
import 'main_playground.dart';
import 'main_processing.dart';
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

/// A sidebar group as in NeoAgent and NeoRecall: one button with an icon;
/// groups with several pages expand into a sub-navigation.
class NavGroup {
  const NavGroup(this.label, this.icon, this.items);

  final String label;
  final IconData icon;
  final List<NavItem> items;

  bool contains(AppPage page) => items.any((item) => item.page == page);
}

List<NavGroup> navigationGroups(BuildContext context, NeoRouterController controller) {
  final t = AppL10n.of(context);
  return <NavGroup>[
    NavGroup(t.groupOverview, Icons.space_dashboard_outlined, <NavItem>[
      NavItem(AppPage.home, t.navHome, Icons.space_dashboard_outlined),
      NavItem(AppPage.activity, t.navActivity, Icons.insights_outlined),
      NavItem(AppPage.playground, t.navPlayground, Icons.forum_outlined),
    ]),
    NavGroup(t.groupModels, Icons.hub_outlined, <NavItem>[
      NavItem(AppPage.models, t.navModels, Icons.view_list_outlined),
      NavItem(AppPage.providers, t.navProviders, Icons.hub_outlined),
    ]),
    NavGroup(t.groupAccess, Icons.alt_route_outlined, <NavItem>[
      NavItem(AppPage.routing, t.navRouting, Icons.alt_route_outlined),
      NavItem(AppPage.keys, t.navKeys, Icons.key_outlined),
      NavItem(AppPage.processing, t.navProcessing, Icons.shield_outlined),
    ]),
    if (controller.user?.isAdmin == true)
      NavGroup(t.navAdmin, Icons.admin_panel_settings_outlined, <NavItem>[
        NavItem(AppPage.admin, t.navAdmin, Icons.admin_panel_settings_outlined),
      ]),
  ];
}

class NeoRouterShell extends StatefulWidget {
  const NeoRouterShell({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<NeoRouterShell> createState() => _NeoRouterShellState();
}

class _NeoRouterShellState extends State<NeoRouterShell> {
  /// The group whose pages are listed; follows navigation like NeoAgent.
  String? _expanded;
  AppPage? _lastPage;

  NeoRouterController get controller => widget.controller;

  void _syncExpanded(List<NavGroup> groups) {
    if (_lastPage == controller.page) return;
    _lastPage = controller.page;
    final group = groups.where((item) => item.contains(controller.page)).firstOrNull;
    if (group != null) _expanded = group.label;
  }

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final groups = navigationGroups(context, controller);
    _syncExpanded(groups);
    final wide = MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet;
    final sidebar = _Sidebar(
      controller: controller,
      groups: groups,
      expanded: _expanded,
      onToggle: (label) => setState(() => _expanded = _expanded == label ? null : label),
    );
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
                title: const BrandLockup(logoSize: 26),
                shape: Border(bottom: BorderSide(color: palette.border)),
              ),
        drawer: wide ? null : Drawer(backgroundColor: palette.surface, shape: const RoundedRectangleBorder(), child: sidebar),
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
        AppPage.processing => ProcessingScreen(controller: controller),
        AppPage.settings => SettingsScreen(controller: controller),
        AppPage.admin => controller.user?.isAdmin == true ? AdminScreen(controller: controller) : DashboardScreen(controller: controller),
      };
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.controller, required this.groups, required this.expanded, required this.onToggle});

  final NeoRouterController controller;
  final List<NavGroup> groups;
  final String? expanded;
  final ValueChanged<String> onToggle;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final t = AppL10n.of(context);
    void go(AppPage page) {
      controller.go(page);
      if (Scaffold.maybeOf(context)?.isDrawerOpen == true) Navigator.of(context).pop();
    }

    final items = <Widget>[];
    for (final group in groups) {
      final parent = group.items.first;
      final hasChildren = group.items.length > 1;
      final open = expanded == group.label;
      items.add(_SidebarButton(
        label: group.label,
        icon: group.icon,
        active: controller.page == parent.page,
        trailing: hasChildren
            ? Icon(open ? Icons.expand_less : Icons.expand_more, size: 16, color: controller.page == parent.page ? palette.textSoft : palette.textMuted)
            : null,
        onTap: hasChildren ? () => onToggle(group.label) : () => go(parent.page),
      ));
      if (hasChildren && open) {
        items.add(_SidebarSubnav(
          children: <Widget>[
            for (final item in group.items)
              _SidebarButton(label: item.label, icon: item.icon, compact: true, active: controller.page == item.page, onTap: () => go(item.page)),
          ],
        ));
      }
    }

    return Container(
      width: 276,
      decoration: BoxDecoration(
        color: palette.surface,
        border: Border(right: BorderSide(color: palette.border)),
      ),
      child: SafeArea(
        child: Column(
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 18, 16, 14),
              child: Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: palette.border),
                  color: palette.surfaceRaised,
                ),
                child: Row(
                  children: <Widget>[
                    const BrandLockup(logoSize: 38, showName: false),
                    const SizedBox(width: 11),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: <Widget>[
                          Text(
                            t.appName,
                            style: GoogleFonts.geist(fontSize: 17, fontWeight: FontWeight.w700, color: palette.text, letterSpacing: -0.35),
                          ),
                          const SizedBox(height: 3),
                          Text(
                            t.brandTagline,
                            style: GoogleFonts.geistMono(fontSize: 9.5, fontWeight: FontWeight.w600, color: palette.textMuted, letterSpacing: 1.8),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
            Expanded(child: ListView(padding: const EdgeInsets.fromLTRB(12, 2, 12, 10), children: items)),
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 14),
              child: _SidebarSettingsButton(
                label: controller.user?.displayName ?? controller.user?.username ?? t.account,
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

class _SidebarSettingsButton extends StatelessWidget {
  const _SidebarSettingsButton({required this.label, required this.active, required this.onTap});

  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final t = AppL10n.of(context);
    final initial = label.trim().isEmpty ? 'N' : label.trim().characters.first.toUpperCase();
    final radius = BorderRadius.circular(18);
    return Material(
      color: active ? palette.accentSoft : palette.surfaceRaised,
      borderRadius: radius,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.fromLTRB(10, 10, 12, 10),
          decoration: BoxDecoration(
            borderRadius: radius,
            border: Border.all(color: active ? palette.accent.withValues(alpha: 0.35) : palette.border),
          ),
          child: Row(
            children: <Widget>[
              Container(
                width: 30,
                height: 30,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: active ? palette.accent : palette.surface,
                  border: Border.all(color: active ? palette.accent : palette.borderStrong),
                ),
                alignment: Alignment.center,
                child: Text(
                  initial,
                  style: TextStyle(color: active ? palette.onAccent : palette.text, fontWeight: FontWeight.w700, fontSize: 12),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: GoogleFonts.geist(color: palette.text, fontSize: 12.5, fontWeight: FontWeight.w600, letterSpacing: -0.1),
                    ),
                    Text(t.navSettings, style: TextStyle(color: palette.textMuted, fontSize: 12)),
                  ],
                ),
              ),
              Icon(Icons.settings_outlined, size: 17, color: active ? palette.accent : palette.textSoft),
            ],
          ),
        ),
      ),
    );
  }
}

class _SidebarButton extends StatefulWidget {
  const _SidebarButton({
    required this.label,
    required this.icon,
    required this.onTap,
    this.active = false,
    this.compact = false,
    this.trailing,
  });

  final String label;
  final IconData icon;
  final VoidCallback onTap;
  final bool active;
  final bool compact;
  final Widget? trailing;

  @override
  State<_SidebarButton> createState() => _SidebarButtonState();
}

class _SidebarButtonState extends State<_SidebarButton> {
  bool _hovering = false;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final active = widget.active;
    final radius = BorderRadius.circular(widget.compact ? 12 : 14);
    // Flat finish as in NeoAgent: the active row is one accent wash plus a
    // gold glyph, no gradient, shadow or rail bar.
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: MouseRegion(
        onEnter: (_) => setState(() => _hovering = true),
        onExit: (_) => setState(() => _hovering = false),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            borderRadius: radius,
            onTap: widget.onTap,
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 160),
              curve: Curves.easeOutCubic,
              width: double.infinity,
              padding: EdgeInsets.symmetric(horizontal: widget.compact ? 10 : 11, vertical: widget.compact ? 7 : 9),
              decoration: BoxDecoration(
                color: active ? palette.accentSoft : (_hovering ? palette.surfaceRaised.withValues(alpha: 0.66) : Colors.transparent),
                borderRadius: radius,
              ),
              child: Row(
                children: <Widget>[
                  Icon(widget.icon, size: widget.compact ? 16 : 18, color: active ? palette.accent : palette.textMuted),
                  SizedBox(width: widget.compact ? 9 : 11),
                  Expanded(
                    child: Text(
                      widget.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: GoogleFonts.geist(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                        letterSpacing: -0.1,
                        color: active ? palette.text : palette.textSoft,
                      ),
                    ),
                  ),
                  if (widget.trailing != null) ...<Widget>[const SizedBox(width: 8), widget.trailing!],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Indented sub-navigation of an expanded group, behind a thin guide line.
class _SidebarSubnav extends StatelessWidget {
  const _SidebarSubnav({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.fromLTRB(14, 2, 0, 2),
      padding: const EdgeInsets.only(left: 12),
      decoration: BoxDecoration(border: Border(left: BorderSide(color: neoRouterPaletteOf(context).border))),
      child: Column(children: children),
    );
  }
}
