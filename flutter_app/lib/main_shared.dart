import 'dart:ui';

import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

/// Flat page backdrop for the app shell, as in NeoAgent's control surface.
class AmbientBackdrop extends StatelessWidget {
  const AmbientBackdrop({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(decoration: BoxDecoration(color: neoRouterPaletteOf(context).scaffold), child: child);
  }
}

/// Backdrop of the sign-in screen: the flat surface with two soft glows.
class AuthBackdrop extends StatelessWidget {
  const AuthBackdrop({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return DecoratedBox(
      decoration: BoxDecoration(gradient: palette.backgroundGradient),
      child: Stack(
        children: <Widget>[
          Positioned(top: -140, left: -80, child: _AuraOrb(color: palette.accent.withValues(alpha: 0.14), size: 320)),
          Positioned(right: -100, bottom: -120, child: _AuraOrb(color: palette.secondary.withValues(alpha: 0.16), size: 340)),
          child,
        ],
      ),
    );
  }
}

class _AuraOrb extends StatelessWidget {
  const _AuraOrb({required this.color, required this.size});

  final Color color;
  final double size;

  @override
  Widget build(BuildContext context) {
    return IgnorePointer(
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: RadialGradient(
            colors: <Color>[
              color,
              color.withValues(alpha: 0.06),
              Colors.transparent,
            ],
            stops: const <double>[0, 0.4, 1],
          ),
        ),
      ),
    );
  }
}

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
    return AnimatedSlide(
      duration: const Duration(milliseconds: 700),
      curve: Curves.easeOutCubic,
      offset: _visible ? Offset.zero : const Offset(0, 0.035),
      child: AnimatedOpacity(
        duration: const Duration(milliseconds: 700),
        curve: Curves.easeOutCubic,
        opacity: _visible ? 1 : 0,
        child: widget.child,
      ),
    );
  }
}

class GlassSurface extends StatelessWidget {
  const GlassSurface({
    super.key,
    required this.child,
    this.width,
    this.padding,
    this.borderRadius = const BorderRadius.all(
      Radius.circular(AppRadius.panel),
    ),
    this.blurSigma = 22,
  });

  final Widget child;
  final double? width;
  final EdgeInsetsGeometry? padding;
  final BorderRadius borderRadius;
  final double blurSigma;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return SizedBox(
      width: width,
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: borderRadius,
          boxShadow: <BoxShadow>[
            BoxShadow(
              color: palette.shadow,
              blurRadius: 42,
              offset: const Offset(0, 22),
            ),
          ],
        ),
        child: ClipRRect(
          borderRadius: borderRadius,
          child: BackdropFilter(
            filter: ImageFilter.blur(sigmaX: blurSigma, sigmaY: blurSigma),
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: palette.glassFill,
                gradient: palette.panelGradient,
                borderRadius: borderRadius,
                border: Border.all(color: palette.glassBorder),
              ),
              child: Padding(padding: padding ?? EdgeInsets.zero, child: child),
            ),
          ),
        ),
      ),
    );
  }
}

class InlineError extends StatelessWidget {
  const InlineError({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.md,
        vertical: AppSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: palette.error.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(AppRadius.input),
        border: Border.all(color: palette.error.withValues(alpha: 0.4)),
      ),
      child: Row(
        children: <Widget>[
          Icon(Icons.error_outline, color: palette.error, size: 18),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Text(
              message,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: palette.error,
                fontSize: 13,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class InlineSuccess extends StatelessWidget {
  const InlineSuccess({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.md,
        vertical: AppSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: palette.success.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(AppRadius.input),
        border: Border.all(color: palette.success.withValues(alpha: 0.4)),
      ),
      child: Row(
        children: <Widget>[
          Icon(Icons.check_circle_outline, color: palette.success, size: 18),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Text(
              message,
              style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                color: palette.success,
                fontSize: 13,
              ),
            ),
          ),
        ],
      ),
    );
  }
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
          child: SvgPicture.asset(
            'assets/branding/logo.svg',
            width: logoSize,
            height: logoSize,
          ),
        ),
        if (showName) ...<Widget>[
          const SizedBox(width: 12),
          Text(
            'NeoRouter',
            style: Theme.of(context).textTheme.titleMedium?.copyWith(
              color: palette.text,
              fontWeight: FontWeight.w800,
              letterSpacing: -0.4,
            ),
          ),
        ],
      ],
    );
  }
}

/// Page title in NeoAgent's layout: a large title, a description and an
/// optional action on the right. No eyebrow; the sidebar already says where
/// you are.
class ScreenHeader extends StatelessWidget {
  const ScreenHeader({super.key, required this.title, required this.description, this.trailing});

  final String title;
  final String description;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final compact = MediaQuery.sizeOf(context).width < 760;
    final titleStyle = displayTitleStyle(palette, size: compact ? 22 : 32);
    final subtitleStyle = TextStyle(color: palette.textSoft, height: compact ? 1.38 : 1.5);
    return EntranceMotion(
      child: Padding(
        padding: EdgeInsets.only(bottom: compact ? 16 : 24),
        child: compact
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(title, maxLines: 1, overflow: TextOverflow.ellipsis, style: titleStyle),
                  const SizedBox(height: 8),
                  ConstrainedBox(constraints: const BoxConstraints(maxWidth: 720), child: Text(description, style: subtitleStyle)),
                  if (trailing != null) ...<Widget>[const SizedBox(height: 12), trailing!],
                ],
              )
            : Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(title, style: titleStyle),
                        const SizedBox(height: 10),
                        ConstrainedBox(constraints: const BoxConstraints(maxWidth: 760), child: Text(description, style: subtitleStyle)),
                      ],
                    ),
                  ),
                  if (trailing != null) trailing!,
                ],
              ),
      ),
    );
  }
}

class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    required this.message,
  });

  final IconData icon;
  final String title;
  final String message;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 420),
          child: Column(
            children: <Widget>[
              Icon(icon, size: 36, color: palette.accent),
              const SizedBox(height: 16),
              Text(title, style: displayTitleStyle(palette, size: 24)),
              const SizedBox(height: 8),
              Text(
                message,
                textAlign: TextAlign.center,
                style: Theme.of(
                  context,
                ).textTheme.bodyMedium?.copyWith(color: palette.textSoft),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.online});

  final bool online;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Container(
      width: 8,
      height: 8,
      decoration: BoxDecoration(
        color: online ? palette.success : palette.textMuted,
        shape: BoxShape.circle,
      ),
    );
  }
}

/// Bordered panel used for every content block on NeoRouter pages. The title
/// sits above the card, like a NeoAgent settings group.
class SectionCard extends StatelessWidget {
  const SectionCard({super.key, required this.child, this.title, this.trailing, this.padding = const EdgeInsets.all(AppSpacing.lg)});

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
            padding: const EdgeInsets.fromLTRB(4, 0, 4, 10),
            child: Row(
              children: <Widget>[
                if (title != null)
                  Expanded(
                    child: Text(
                      title!,
                      style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700, color: palette.text),
                    ),
                  )
                else
                  const Spacer(),
                if (trailing != null) trailing!,
              ],
            ),
          ),
        Container(
          decoration: BoxDecoration(
            color: palette.surface,
            borderRadius: BorderRadius.circular(AppRadius.card),
            border: Border.all(color: palette.border),
          ),
          padding: padding,
          child: child,
        ),
      ],
    );
  }
}

enum PillTone { neutral, accent, success, warning, error, info }

class MetaPill extends StatelessWidget {
  const MetaPill({super.key, required this.label, this.tone = PillTone.neutral, this.icon});

  final String label;
  final PillTone tone;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final color = switch (tone) {
      PillTone.neutral => palette.textSoft,
      PillTone.accent => palette.accentStrong,
      PillTone.success => palette.success,
      PillTone.warning => palette.warning,
      PillTone.error => palette.error,
      PillTone.info => palette.info,
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: tone == PillTone.neutral ? palette.surfaceMuted : color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(AppRadius.pill),
        border: Border.all(color: tone == PillTone.neutral ? palette.border : color.withValues(alpha: 0.35)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          if (icon != null) ...<Widget>[Icon(icon, size: 12, color: color), const SizedBox(width: 4)],
          Text(label, style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }
}

class StatTile extends StatelessWidget {
  const StatTile({super.key, required this.label, required this.value, this.caption, this.icon});

  final String label;
  final String value;
  final String? caption;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return SectionCard(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              if (icon != null) ...<Widget>[Icon(icon, size: 16, color: palette.accent), const SizedBox(width: 6)],
              Expanded(
                child: Text(label, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textMuted, fontSize: 12, fontWeight: FontWeight.w600)),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.xs),
          Text(value, style: displayTitleStyle(palette, size: 26)),
          const SizedBox(height: 4),
          Text(caption ?? ' ', maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textSoft, fontSize: 12)),
        ],
      ),
    );
  }
}

/// Horizontal usage bar; turns warning/error colored as it fills up.
class UsageBar extends StatelessWidget {
  const UsageBar({super.key, required this.fraction, this.height = 8});

  final double fraction;
  final double height;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final value = fraction.clamp(0.0, 1.0);
    final color = value >= 0.9 ? palette.error : value >= 0.7 ? palette.warning : palette.accent;
    return ClipRRect(
      borderRadius: BorderRadius.circular(AppRadius.pill),
      child: LinearProgressIndicator(
        minHeight: height,
        value: value,
        backgroundColor: palette.surfaceMuted,
        valueColor: AlwaysStoppedAnimation<Color>(color),
      ),
    );
  }
}

/// Labeled 0..1 score meter used for intelligence, speed and reliability.
class ScoreMeter extends StatelessWidget {
  const ScoreMeter({super.key, required this.label, required this.value});

  final String label;
  final double? value;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(label, style: TextStyle(color: palette.textMuted, fontSize: 12, fontWeight: FontWeight.w600)),
        const SizedBox(height: 4),
        SizedBox(
          width: 72,
          child: ClipRRect(
            borderRadius: BorderRadius.circular(AppRadius.pill),
            child: LinearProgressIndicator(
              minHeight: 5,
              value: (value ?? 0).clamp(0.0, 1.0),
              backgroundColor: palette.surfaceMuted,
              valueColor: AlwaysStoppedAnimation<Color>(palette.secondary),
            ),
          ),
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
    if (loading) return const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator()));
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
    return child;
  }
}

/// Scrollable page frame: header plus content with consistent spacing.
class PageScaffold extends StatelessWidget {
  const PageScaffold({super.key, required this.header, required this.children});

  final Widget header;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: pagePadding(context),
      children: <Widget>[
        header,
        for (var index = 0; index < children.length; index++) ...<Widget>[
          if (index > 0) const SizedBox(height: AppSpacing.md),
          children[index],
        ],
        const SizedBox(height: AppSpacing.xl),
      ],
    );
  }
}

Future<bool> confirmAction(BuildContext context, {required String title, required String message, required String confirmLabel, required String cancelLabel}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(title),
      content: SizedBox(width: 420, child: Text(message)),
      actions: <Widget>[
        OutlinedButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(cancelLabel)),
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(confirmLabel)),
      ],
    ),
  );
  return result == true;
}

void showSnack(BuildContext context, String message) {
  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message), behavior: SnackBarBehavior.floating));
}

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
  return '${two(time.day)}.${two(time.month)}. ${two(time.hour)}:${two(time.minute)}:${two(time.second)}';
}

/// The heading over a group of settings rows, as in NeoAgent.
class SettingsHeading extends StatelessWidget {
  const SettingsHeading({super.key, required this.title, this.description, this.trailing});

  final String title;
  final String? description;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 0, 4, 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w700, color: palette.text)),
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
    return Container(
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(AppRadius.card),
        border: Border.all(color: palette.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          for (var i = 0; i < children.length; i++) ...<Widget>[
            if (i > 0) Divider(height: 1, thickness: 1, color: palette.border),
            children[i],
          ],
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
        Text(label, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text)),
        if (description != null) ...<Widget>[
          const SizedBox(height: 4),
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
              children: <Widget>[text, const SizedBox(height: 12), Align(alignment: Alignment.centerLeft, child: control)],
            );
          }
          return Row(children: <Widget>[Expanded(child: text), const SizedBox(width: 24), control]);
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
