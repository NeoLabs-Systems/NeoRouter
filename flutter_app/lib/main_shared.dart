import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

/// Flat page backdrop for the app shell.
class AmbientBackdrop extends StatelessWidget {
  const AmbientBackdrop({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(color: neoRouterPaletteOf(context).scaffold),
      child: child,
    );
  }
}

/// Backdrop of the sign-in screens: the same flat surface as the app.
class AuthBackdrop extends StatelessWidget {
  const AuthBackdrop({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => AmbientBackdrop(child: child);
}

/// A short fade-in for content that appears after loading.
class EntranceMotion extends StatefulWidget {
  const EntranceMotion({super.key, required this.child});

  final Widget child;

  @override
  State<EntranceMotion> createState() => _EntranceMotionState();
}

class _EntranceMotionState extends State<EntranceMotion> {
  bool _visible = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) setState(() => _visible = true);
    });
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedOpacity(duration: const Duration(milliseconds: 220), curve: Curves.easeOut, opacity: _visible ? 1 : 0, child: widget.child);
  }
}

/// Raised card for standalone forms (sign-in, dialogs drawn in place).
class GlassSurface extends StatelessWidget {
  const GlassSurface({
    super.key,
    required this.child,
    this.width,
    this.padding,
    this.borderRadius = const BorderRadius.all(Radius.circular(AppRadius.panel)),
    this.blurSigma = 0,
  });

  final Widget child;
  final double? width;
  final EdgeInsetsGeometry? padding;
  final BorderRadius borderRadius;
  final double blurSigma;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      width: width,
      padding: padding,
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: borderRadius,
        border: Border.all(color: palette.border),
        boxShadow: <BoxShadow>[BoxShadow(color: palette.shadow, blurRadius: 32, offset: const Offset(0, 12))],
      ),
      child: child,
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.message, required this.color, required this.icon});

  final String message;
  final Color color;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 11),
      decoration: BoxDecoration(color: color.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Padding(
            padding: const EdgeInsets.only(top: 1),
            child: Icon(icon, color: color, size: 17),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(message, style: TextStyle(color: palette.text, fontSize: 13, height: 1.4)),
          ),
        ],
      ),
    );
  }
}

class InlineError extends StatelessWidget {
  const InlineError({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => _Notice(message: message, color: neoRouterPaletteOf(context).error, icon: Icons.error_outline_rounded);
}

class InlineSuccess extends StatelessWidget {
  const InlineSuccess({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => _Notice(message: message, color: neoRouterPaletteOf(context).success, icon: Icons.check_circle_outline_rounded);
}

/// Neutral hint box, e.g. "coming soon" or a short explanation.
class InlineHint extends StatelessWidget {
  const InlineHint({super.key, required this.message, this.icon = Icons.info_outline_rounded});

  final String message;
  final IconData icon;

  @override
  Widget build(BuildContext context) => _Notice(message: message, color: neoRouterPaletteOf(context).textSoft, icon: icon);
}

class BrandLockup extends StatelessWidget {
  const BrandLockup({super.key, this.logoSize = 48, this.showName = true});

  final double logoSize;
  final bool showName;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        ClipRRect(
          borderRadius: BorderRadius.circular(logoSize * 0.24),
          child: SvgPicture.asset('assets/branding/logo.svg', width: logoSize, height: logoSize),
        ),
        if (showName) ...<Widget>[
          const SizedBox(width: 10),
          Text(
            'NeoRouter',
            style: TextStyle(color: palette.text, fontSize: 15.5, fontWeight: FontWeight.w700, letterSpacing: -0.3),
          ),
        ],
      ],
    );
  }
}

/// Page title, one line of description and the page's main actions.
class ScreenHeader extends StatelessWidget {
  const ScreenHeader({super.key, required this.title, required this.description, this.trailing});

  final String title;
  final String description;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final compact = MediaQuery.sizeOf(context).width < 760;
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(title, style: displayTitleStyle(palette, size: compact ? 24 : 28)),
        if (description.isNotEmpty) ...<Widget>[
          const SizedBox(height: 6),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Text(description, style: TextStyle(color: palette.textSoft, fontSize: 14.5, height: 1.45)),
          ),
        ],
      ],
    );
    return Padding(
      padding: EdgeInsets.only(bottom: compact ? 20 : 28),
      child: compact
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                heading,
                if (trailing != null) ...<Widget>[const SizedBox(height: 14), trailing!],
              ],
            )
          : Row(
              crossAxisAlignment: CrossAxisAlignment.end,
              children: <Widget>[
                Expanded(child: heading),
                if (trailing != null) ...<Widget>[const SizedBox(width: 16), trailing!],
              ],
            ),
    );
  }
}

class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.icon, required this.title, required this.message, this.action});

  final IconData icon;
  final String title;
  final String message;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48, horizontal: 16),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 400),
          child: Column(
            children: <Widget>[
              Container(
                width: 48,
                height: 48,
                decoration: BoxDecoration(color: palette.surfaceRaised, shape: BoxShape.circle),
                child: Icon(icon, size: 22, color: palette.textSoft),
              ),
              const SizedBox(height: 16),
              Text(title, textAlign: TextAlign.center, style: displayTitleStyle(palette, size: 18)),
              const SizedBox(height: 6),
              Text(
                message,
                textAlign: TextAlign.center,
                style: TextStyle(color: palette.textSoft, fontSize: 14, height: 1.45),
              ),
              if (action != null) ...<Widget>[const SizedBox(height: 18), action!],
            ],
          ),
        ),
      ),
    );
  }
}

/// Severity of a status. Shown as a small dot only.
enum PillTone { neutral, accent, success, warning, error, info }

Color toneColor(NeoRouterPalette palette, PillTone tone) => switch (tone) {
  PillTone.success => palette.success,
  PillTone.warning => palette.warning,
  PillTone.error => palette.error,
  PillTone.accent => palette.accent,
  PillTone.neutral || PillTone.info => palette.textMuted,
};

class ToneDot extends StatelessWidget {
  const ToneDot({super.key, required this.tone, this.size = 7});

  final PillTone tone;
  final double size;

  @override
  Widget build(BuildContext context) => Container(
    width: size,
    height: size,
    decoration: BoxDecoration(color: toneColor(neoRouterPaletteOf(context), tone), shape: BoxShape.circle),
  );
}

class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.online});

  final bool online;

  @override
  Widget build(BuildContext context) => ToneDot(tone: online ? PillTone.success : PillTone.neutral);
}

/// Dot plus a short status word, e.g. "● Working".
class StatusLabel extends StatelessWidget {
  const StatusLabel({super.key, required this.label, required this.tone});

  final String label;
  final PillTone tone;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        ToneDot(tone: tone),
        const SizedBox(width: 7),
        Text(label, style: TextStyle(color: palette.textSoft, fontSize: 13)),
      ],
    );
  }
}

/// Bordered panel for every content block. The title sits above the card.
class SectionCard extends StatelessWidget {
  const SectionCard({super.key, required this.child, this.title, this.trailing, this.padding = const EdgeInsets.all(20)});

  final Widget child;
  final String? title;
  final Widget? trailing;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (title != null || trailing != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(2, 0, 0, 10),
            child: Row(
              children: <Widget>[
                Expanded(child: title == null ? const SizedBox() : Text(title!, style: sectionTitleStyle(palette))),
                if (trailing != null) trailing!,
              ],
            ),
          ),
        AppCard(padding: padding, child: child),
      ],
    );
  }
}

/// The one card style: surface, hairline border, no shadow.
class AppCard extends StatelessWidget {
  const AppCard({super.key, required this.child, this.padding = const EdgeInsets.all(20), this.onTap});

  final Widget child;
  final EdgeInsetsGeometry padding;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final radius = BorderRadius.circular(AppRadius.card);
    return Material(
      color: palette.surface,
      shape: RoundedRectangleBorder(
        borderRadius: radius,
        side: BorderSide(color: palette.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: onTap == null
          ? Padding(padding: padding, child: child)
          : InkWell(
              onTap: onTap,
              child: Padding(padding: padding, child: child),
            ),
    );
  }
}

/// Small neutral tag. A non-neutral [tone] adds a status dot; the color never
/// fills the tag.
class MetaPill extends StatelessWidget {
  const MetaPill({super.key, required this.label, this.tone = PillTone.neutral, this.icon});

  final String label;
  final PillTone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final dot = tone == PillTone.success || tone == PillTone.warning || tone == PillTone.error;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.tag)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          if (dot) ...<Widget>[ToneDot(tone: tone, size: 6), const SizedBox(width: 6)] else if (icon != null) ...<Widget>[
            Icon(icon, size: 12, color: palette.textMuted),
            const SizedBox(width: 5),
          ],
          Text(
            label,
            style: TextStyle(
              color: tone == PillTone.accent ? palette.text : palette.textSoft,
              fontSize: 12,
              fontWeight: tone == PillTone.accent ? FontWeight.w600 : FontWeight.w500,
            ),
          ),
        ],
      ),
    );
  }
}

/// A single number with its label.
class StatTile extends StatelessWidget {
  const StatTile({super.key, required this.label, required this.value, this.caption, this.icon});

  final String label;
  final String value;
  final String? caption;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    return AppCard(
      padding: const EdgeInsets.all(18),
      child: _Metric(label: label, value: value, caption: caption),
    );
  }
}

class _Metric extends StatelessWidget {
  const _Metric({required this.label, required this.value, this.caption});

  final String label;
  final String value;
  final String? caption;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(
          label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(color: palette.textMuted, fontSize: 12.5),
        ),
        const SizedBox(height: 6),
        Text(value, maxLines: 1, overflow: TextOverflow.ellipsis, style: displayTitleStyle(palette, size: 24)),
        if (caption != null) ...<Widget>[
          const SizedBox(height: 3),
          Text(
            caption!,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(color: palette.textSoft, fontSize: 12.5),
          ),
        ],
      ],
    );
  }
}

/// Several numbers on one card, split by hairlines; wraps to two columns on
/// narrow screens.
class MetricStrip extends StatelessWidget {
  const MetricStrip({super.key, required this.metrics});

  final List<({String label, String value, String? caption})> metrics;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return AppCard(
      padding: EdgeInsets.zero,
      child: LayoutBuilder(
        builder: (context, constraints) {
          final perRow = constraints.maxWidth < 560 ? 2 : metrics.length;
          final rows = <List<Widget>>[];
          for (var i = 0; i < metrics.length; i += perRow) {
            rows.add(<Widget>[
              for (final metric in metrics.skip(i).take(perRow))
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(20, 18, 16, 18),
                    child: _Metric(label: metric.label, value: metric.value, caption: metric.caption),
                  ),
                ),
            ]);
          }
          return Column(
            children: <Widget>[
              for (var r = 0; r < rows.length; r++) ...<Widget>[
                if (r > 0) Divider(height: 1, color: palette.border),
                IntrinsicHeight(
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      for (var c = 0; c < rows[r].length; c++) ...<Widget>[if (c > 0) VerticalDivider(width: 1, color: palette.border), rows[r][c]],
                    ],
                  ),
                ),
              ],
            ],
          );
        },
      ),
    );
  }
}

/// Text tabs with an underline, for switching views inside a page.
class PageTabs<T> extends StatelessWidget {
  const PageTabs({super.key, required this.tabs, required this.selected, required this.onChanged});

  final List<({T value, String label, int? count})> tabs;
  final T selected;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      decoration: BoxDecoration(
        border: Border(bottom: BorderSide(color: palette.border)),
      ),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: <Widget>[
            for (final tab in tabs)
              InkWell(
                onTap: () => onChanged(tab.value),
                borderRadius: BorderRadius.circular(AppRadius.tag),
                child: Container(
                  padding: const EdgeInsets.fromLTRB(2, 8, 2, 10),
                  margin: const EdgeInsets.only(right: 22),
                  decoration: BoxDecoration(
                    border: Border(bottom: BorderSide(color: tab.value == selected ? palette.text : Colors.transparent, width: 2)),
                  ),
                  child: Row(
                    children: <Widget>[
                      Text(
                        tab.label,
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: tab.value == selected ? FontWeight.w600 : FontWeight.w500,
                          color: tab.value == selected ? palette.text : palette.textMuted,
                        ),
                      ),
                      if (tab.count != null) ...<Widget>[
                        const SizedBox(width: 6),
                        Text('${tab.count}', style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                      ],
                    ],
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// Neutral rounded square with a provider's or model's initials.
class ProviderAvatar extends StatelessWidget {
  const ProviderAvatar({super.key, required this.label, this.size = 34});

  final String label;
  final double size;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final words = label.trim().split(RegExp(r'[\s/_-]+')).where((word) => word.isNotEmpty).toList();
    final initials = words.isEmpty
        ? '?'
        : (words.length == 1 ? words.first.characters.take(2).toString() : '${words[0].characters.first}${words[1].characters.first}');
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: palette.surfaceRaised,
        borderRadius: BorderRadius.circular(size * 0.28),
        border: Border.all(color: palette.border),
      ),
      child: Text(
        initials.toUpperCase(),
        style: TextStyle(fontSize: size * 0.36, fontWeight: FontWeight.w600, color: palette.textSoft, letterSpacing: -0.2),
      ),
    );
  }
}

/// One row of a list card: optional leading, title, subtitle, trailing.
class ListRow extends StatelessWidget {
  const ListRow({super.key, this.leading, required this.title, this.subtitle, this.trailing, this.onTap, this.padding});

  final Widget? leading;
  final String title;
  final String? subtitle;
  final Widget? trailing;
  final VoidCallback? onTap;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final content = Padding(
      padding: padding ?? const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Row(
        children: <Widget>[
          if (leading != null) ...<Widget>[leading!, const SizedBox(width: 14)],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: palette.text, fontSize: 14, fontWeight: FontWeight.w500),
                ),
                if (subtitle != null && subtitle!.isNotEmpty) ...<Widget>[
                  const SizedBox(height: 2),
                  Text(
                    subtitle!,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: palette.textMuted, fontSize: 12.5),
                  ),
                ],
              ],
            ),
          ),
          if (trailing != null) ...<Widget>[const SizedBox(width: 12), trailing!],
        ],
      ),
    );
    return onTap == null ? content : InkWell(onTap: onTap, child: content);
  }
}

/// Rows on one card split by hairlines.
class ListCard extends StatelessWidget {
  const ListCard({super.key, required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) => SettingsCardList(children: children);
}

/// Thin progress bar; turns amber only when nearly full.
class UsageBar extends StatelessWidget {
  const UsageBar({super.key, required this.fraction, this.height = 6});

  final double fraction;
  final double height;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final value = fraction.clamp(0.0, 1.0);
    return ClipRRect(
      borderRadius: BorderRadius.circular(AppRadius.pill),
      child: LinearProgressIndicator(
        minHeight: height,
        value: value,
        backgroundColor: palette.surfaceRaised,
        valueColor: AlwaysStoppedAnimation<Color>(value >= 0.9 ? palette.warning : palette.textSoft),
      ),
    );
  }
}

/// Labeled 0..1 score as five small segments.
class ScoreMeter extends StatelessWidget {
  const ScoreMeter({super.key, required this.label, required this.value});

  final String label;
  final double? value;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final filled = value == null ? 0 : (value!.clamp(0.0, 1.0) * 5).round();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(label, style: TextStyle(color: palette.textMuted, fontSize: 12)),
        const SizedBox(height: 5),
        Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            for (var i = 0; i < 5; i++)
              Container(
                width: 12,
                height: 4,
                margin: const EdgeInsets.only(right: 2),
                decoration: BoxDecoration(
                  color: i < filled ? palette.text.withValues(alpha: 0.7) : palette.surfaceRaised,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
          ],
        ),
      ],
    );
  }
}

/// Loading / error / content switch for pages that fetch data on open.
class AsyncBody extends StatelessWidget {
  const AsyncBody({super.key, required this.loading, required this.error, required this.onRetry, required this.child});

  final bool loading;
  final String? error;
  final VoidCallback onRetry;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    if (loading) {
      return const Padding(
        padding: EdgeInsets.all(64),
        child: Center(child: SizedBox.square(dimension: 22, child: CircularProgressIndicator(strokeWidth: 2))),
      );
    }
    if (error != null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          InlineError(message: error!),
          const SizedBox(height: AppSpacing.sm),
          OutlinedButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh, size: 16), label: Text(AppL10n.of(context).retry)),
        ],
      );
    }
    return EntranceMotion(child: child);
  }
}

/// Scrollable page frame: header plus content, centered at a readable width.
class PageScaffold extends StatelessWidget {
  const PageScaffold({super.key, required this.header, required this.children, this.gap = 20});

  final Widget header;
  final List<Widget> children;
  final double gap;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: pagePadding(context),
      children: <Widget>[
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: kContentMaxWidth),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                header,
                for (var index = 0; index < children.length; index++) ...<Widget>[if (index > 0) SizedBox(height: gap), children[index]],
              ],
            ),
          ),
        ),
      ],
    );
  }
}

Future<bool> confirmAction(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  required String cancelLabel,
  bool destructive = true,
}) async {
  final palette = neoRouterPaletteOf(context);
  final result = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(title),
      content: SizedBox(
        width: 400,
        child: Text(message, style: TextStyle(color: palette.textSoft, height: 1.45)),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(cancelLabel)),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          style: destructive ? FilledButton.styleFrom(backgroundColor: palette.error, foregroundColor: Colors.white) : null,
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return result == true;
}

void showSnack(BuildContext context, String message) {
  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message), behavior: SnackBarBehavior.floating, width: 420));
}

/// "Created by Anna", or "Created by a former member" once that account is gone.
String createdByLabel(AppL10n t, TeamUser? user) => t.createdBy(user?.displayName ?? t.formerMember);

String formatUsd(double value) {
  if (value == 0) return r'$0';
  if (value < 0.01) return '\$${value.toStringAsFixed(4)}';
  return '\$${value.toStringAsFixed(2)}';
}

String formatPricePerMillion(double? value) => value == null ? '—' : '\$${value < 1 ? value.toStringAsFixed(3) : value.toStringAsFixed(2)}';

String formatCount(num value) {
  if (value >= 1e9) return '${(value / 1e9).toStringAsFixed(1)}B';
  if (value >= 1e6) return '${(value / 1e6).toStringAsFixed(1)}M';
  if (value >= 1e3) return '${(value / 1e3).toStringAsFixed(1)}k';
  return value.round().toString();
}

String formatMs(num? value) {
  if (value == null) return '—';
  if (value >= 10000) return '${(value / 1000).toStringAsFixed(0)} s';
  if (value >= 1000) return '${(value / 1000).toStringAsFixed(1)} s';
  return '${value.round()} ms';
}

String formatTime(DateTime time) {
  String two(int value) => value.toString().padLeft(2, '0');
  return '${two(time.day)}.${two(time.month)}. ${two(time.hour)}:${two(time.minute)}';
}

/// "just now", "5 min ago", "3 h ago", else the date.
String formatAgo(AppL10n t, DateTime time) {
  final diff = DateTime.now().difference(time);
  if (diff.inMinutes < 1) return t.agoNow;
  if (diff.inMinutes < 60) return t.agoMinutes('${diff.inMinutes}');
  if (diff.inHours < 24) return t.agoHours('${diff.inHours}');
  return formatTime(time);
}

/// A model id without its provider prefix and vendor folder, for display:
/// "groq/qwen/qwen3.8-27b" → "qwen3.8-27b".
String shortModelName(String id) {
  final last = id.split('/').last;
  return last.replaceFirst(RegExp(r':free$'), '');
}

/// The heading over a group of settings rows.
class SettingsHeading extends StatelessWidget {
  const SettingsHeading({super.key, required this.title, this.description, this.trailing});

  final String title;
  final String? description;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(2, 0, 0, 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: sectionTitleStyle(palette)),
                if (description != null) ...<Widget>[
                  const SizedBox(height: 3),
                  Text(description!, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
                ],
              ],
            ),
          ),
          if (trailing != null) ...<Widget>[const SizedBox(width: 12), trailing!],
        ],
      ),
    );
  }
}

/// Rows on a single card, split by hairlines.
class SettingsCardList extends StatelessWidget {
  const SettingsCardList({super.key, required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return AppCard(
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          for (var i = 0; i < children.length; i++) ...<Widget>[if (i > 0) Divider(height: 1, thickness: 1, color: palette.border), children[i]],
        ],
      ),
    );
  }
}

/// One setting: label and help on the left, its control on the right. On a
/// narrow screen the control moves under the label.
class SettingsRow extends StatelessWidget {
  const SettingsRow({super.key, required this.label, this.description, this.control});

  final String label;
  final String? description;
  final Widget? control;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          label,
          style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: palette.text),
        ),
        if (description != null) ...<Widget>[
          const SizedBox(height: 3),
          Text(description!, style: TextStyle(fontSize: 13, color: palette.textSoft, height: 1.45)),
        ],
      ],
    );
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final control = this.control;
          if (control == null) return text;
          if (constraints.maxWidth < 560) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                text,
                const SizedBox(height: 12),
                Align(alignment: Alignment.centerLeft, child: control),
              ],
            );
          }
          return Row(
            children: <Widget>[
              Expanded(child: text),
              const SizedBox(width: 24),
              control,
            ],
          );
        },
      ),
    );
  }
}

/// A titled block of rows on one card with the standard gap below.
class SettingsGroup extends StatelessWidget {
  const SettingsGroup({super.key, required this.title, required this.children, this.description, this.trailing});

  final String title;
  final String? description;
  final Widget? trailing;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          SettingsHeading(title: title, description: description, trailing: trailing),
          if (children.isNotEmpty) SettingsCardList(children: children),
        ],
      ),
    );
  }
}

/// A selectable card with a title and a one-line explanation.
class OptionCard extends StatelessWidget {
  const OptionCard({super.key, required this.title, required this.description, required this.selected, required this.onTap});

  final String title;
  final String description;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Material(
        color: selected ? palette.surfaceRaised : Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.control),
          side: BorderSide(color: selected ? palette.text.withValues(alpha: 0.5) : palette.borderStrong),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(AppRadius.control),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.all(14),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Icon(
                  selected ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded,
                  size: 18,
                  color: selected ? palette.accent : palette.textMuted,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(
                        title,
                        style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
                      ),
                      const SizedBox(height: 2),
                      Text(description, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.4)),
                    ],
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
