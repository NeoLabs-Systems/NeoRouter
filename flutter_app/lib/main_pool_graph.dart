import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import 'main_theme.dart';

/// One member around the pool.
class PoolGraphMember {
  const PoolGraphMember({
    required this.id,
    required this.name,
    required this.label,
    required this.caption,
    required this.isYou,
    required this.state,
    required this.share,
    required this.gives,
    required this.takes,
  });

  final String id;
  final String name;
  final String label;
  final String caption;
  final bool isYou;

  /// online, offline, paused or connecting.
  final String state;

  /// Its part of the models shared in this pool, 0–1 (the ring).
  final double share;

  /// What it gave into the pool and took from it today (requests).
  final int gives;
  final int takes;
}

/// The pool as a basin: every member sits around it, what they give flows in
/// (gold), what they take flows out (blue), and the basin's level is what is
/// still free today. Online members are linked directly to each other (faint
/// lines), as the pool really is. The motion is drawn on one canvas; members
/// are real buttons over it.
class PoolGraph extends StatefulWidget {
  const PoolGraph({
    super.key,
    required this.members,
    required this.level,
    required this.levelLabel,
    required this.levelCaption,
    required this.footnote,
    required this.active,
    required this.selectedId,
    required this.onSelect,
    this.height = 520,
  });

  final List<PoolGraphMember> members;

  /// 0–1: how full the basin is.
  final double level;
  final String levelLabel;
  final String levelCaption;
  final String footnote;
  final bool active;
  final String? selectedId;
  final ValueChanged<String> onSelect;
  final double height;

  static const Color give = Color(0xFFE0B85C);
  static const Color take = Color(0xFF7FB3E0);

  @override
  State<PoolGraph> createState() => _PoolGraphState();
}

class _PoolGraphState extends State<PoolGraph> with SingleTickerProviderStateMixin {
  late final Ticker _ticker;
  final ValueNotifier<double> _time = ValueNotifier<double>(0);

  @override
  void initState() {
    super.initState();
    _ticker = createTicker((elapsed) => _time.value = elapsed.inMicroseconds / 1e6)..start();
  }

  @override
  void dispose() {
    _ticker.dispose();
    _time.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    // With reduced motion the picture stays still.
    final still = MediaQuery.maybeDisableAnimationsOf(context) == true;
    if (still && _ticker.isActive) _ticker.stop();
    if (!still && !_ticker.isActive) _ticker.start();
    return SizedBox(
      height: widget.height,
      child: LayoutBuilder(
        builder: (context, box) {
          final layout = _Layout.of(box.biggest, widget.members.length);
          return Stack(
            clipBehavior: Clip.none,
            children: <Widget>[
              Positioned.fill(
                child: RepaintBoundary(
                  child: CustomPaint(
                    painter: _PoolPainter(time: _time, layout: layout, members: widget.members, level: widget.level, active: widget.active, palette: palette),
                  ),
                ),
              ),
              Positioned(
                left: layout.center.dx - layout.basin,
                top: layout.center.dy - layout.basin,
                width: layout.basin * 2,
                height: layout.basin * 2,
                child: IgnorePointer(
                  child: Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: <Widget>[
                        Text(widget.levelCaption, style: TextStyle(fontSize: layout.compact ? 9 : 10.5, letterSpacing: 1.2, color: palette.textSoft, fontWeight: FontWeight.w600)),
                        Text(widget.levelLabel, style: TextStyle(fontSize: layout.compact ? 22 : 32, fontWeight: FontWeight.w700, color: palette.text, height: 1.15)),
                        if (!layout.compact) Text(widget.footnote, style: TextStyle(fontSize: 11.5, color: palette.textSoft)),
                      ],
                    ),
                  ),
                ),
              ),
              for (var i = 0; i < widget.members.length; i++)
                Positioned(
                  left: layout.nodes[i].dx - layout.nodeWidth / 2,
                  // A member in the upper part has its name above, away from the
                  // lines and requests running down to the basin.
                  top: layout.nodes[i].dy - layout.avatar / 2 - (layout.labelAbove(i) ? layout.labelHeight : 0),
                  width: layout.nodeWidth,
                  child: _MemberNode(
                    member: widget.members[i],
                    size: layout.avatar,
                    compact: layout.compact,
                    active: widget.active,
                    selected: widget.members[i].id == widget.selectedId,
                    labelAbove: layout.labelAbove(i),
                    labelHeight: layout.labelHeight,
                    onTap: () => widget.onSelect(widget.members[i].id),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}

class _Layout {
  _Layout({required this.size, required this.center, required this.radius, required this.basin, required this.avatar, required this.nodeWidth, required this.compact, required this.nodes});

  factory _Layout.of(Size size, int count) {
    final compact = size.width < 520;
    final avatar = compact ? 44.0 : 60.0;
    final center = Offset(size.width / 2, size.height / 2);
    final radius = math.max(96.0, math.min(size.width / 2 - (compact ? 40 : 80), size.height / 2 - (compact ? 50 : 72)));
    final basin = compact ? 52.0 : 80.0;
    final nodes = <Offset>[
      for (var i = 0; i < count; i++) center + Offset.fromDirection(-math.pi / 2 + i * 2 * math.pi / math.max(1, count), radius),
    ];
    return _Layout(size: size, center: center, radius: radius, basin: basin, avatar: avatar, nodeWidth: compact ? 76 : 150, compact: compact, nodes: nodes);
  }

  final Size size;
  final Offset center;
  final double radius;
  final double basin;
  final double avatar;
  final double nodeWidth;
  final bool compact;
  final List<Offset> nodes;

  double get labelHeight => compact ? 22 : 40;

  bool labelAbove(int index) => nodes[index].dy < center.dy - radius * 0.3;
}

class _PoolPainter extends CustomPainter {
  _PoolPainter({required this.time, required this.layout, required this.members, required this.level, required this.active, required this.palette}) : super(repaint: time);

  final ValueNotifier<double> time;
  final _Layout layout;
  final List<PoolGraphMember> members;
  final double level;
  final bool active;
  final NeoRouterPalette palette;

  bool _flowing(PoolGraphMember member) => active && member.state == 'online';

  @override
  void paint(Canvas canvas, Size size) {
    final t = time.value;
    final center = layout.center;
    final maxVolume = members.fold<int>(1, (top, member) => math.max(top, math.max(member.gives, member.takes)));

    // Direct links between online members.
    final online = <Offset>[for (var i = 0; i < members.length; i++) if (_flowing(members[i])) layout.nodes[i]];
    final mesh = Paint()
      ..color = palette.border.withValues(alpha: 0.45)
      ..strokeWidth = 1;
    for (var a = 0; a < online.length; a++) {
      for (var b = a + 1; b < online.length; b++) {
        canvas.drawLine(online[a], online[b], mesh);
      }
    }

    // A line from each member to the basin.
    for (var i = 0; i < members.length; i++) {
      final member = members[i];
      final from = layout.nodes[i];
      if (member.state == 'offline') {
        _dashed(canvas, from, center, Paint()
          ..color = palette.textMuted.withValues(alpha: 0.6)
          ..strokeWidth = 1.4);
        continue;
      }
      final paint = Paint()..strokeWidth = 2;
      if (_flowing(member)) {
        paint.shader = LinearGradient(colors: <Color>[PoolGraph.give.withValues(alpha: 0.35), PoolGraph.take.withValues(alpha: 0.25)]).createShader(Rect.fromPoints(from, center));
      } else {
        paint.color = palette.border;
      }
      canvas.drawLine(from, center, paint);
    }

    _basin(canvas, t);

    // Requests on their way: in (gives) on one lane, out (takes) on the other.
    for (var i = 0; i < members.length; i++) {
      final member = members[i];
      if (!_flowing(member)) continue;
      final node = layout.nodes[i];
      final direction = (center - node) / (center - node).distance;
      final normal = Offset(-direction.dy, direction.dx) * (layout.compact ? 5 : 6.5);
      // A name under the avatar sits on the way down: start below it.
      final labelGap = !layout.labelAbove(i) && direction.dy > 0.6 ? layout.labelHeight : 0;
      final start = node + direction * (layout.avatar / 2 + 10 + labelGap);
      final end = center - direction * (layout.basin + 6);
      _lane(canvas, t, start + normal, end + normal, member.gives, maxVolume, PoolGraph.give, 0);
      _lane(canvas, t, end - normal, start - normal, member.takes, maxVolume, PoolGraph.take, 0.35);
    }
  }

  void _lane(Canvas canvas, double t, Offset from, Offset to, int volume, int maxVolume, Color color, double phase) {
    if (volume <= 0) return;
    final share = volume / maxVolume;
    final count = math.max(1, (share * 4).round());
    final duration = math.max(1.4, 3.6 - share * 1.8);
    final radius = layout.compact ? 2.8 : 3.4;
    final glow = Paint()..maskFilter = const MaskFilter.blur(BlurStyle.normal, 5);
    for (var k = 0; k < count; k++) {
      final progress = ((t + phase) / duration + k / count) % 1.0;
      final fade = progress < 0.12 ? progress / 0.12 : progress > 0.88 ? (1 - progress) / 0.12 : 1.0;
      final point = Offset.lerp(from, to, progress)!;
      canvas.drawCircle(point, radius * 2.2, glow..color = color.withValues(alpha: 0.35 * fade));
      canvas.drawCircle(point, radius, Paint()..color = color.withValues(alpha: fade));
    }
  }

  void _basin(Canvas canvas, double t) {
    final center = layout.center;
    final r = layout.basin;
    if (active) {
      final pulse = (math.sin(t * 2 * math.pi / 3.2) + 1) / 2;
      canvas.drawCircle(center, r + 3 + pulse * 10, Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2
        ..color = PoolGraph.give.withValues(alpha: 0.22 * (1 - pulse)));
    }
    canvas.drawCircle(center, r, Paint()..color = palette.surfaceRaised);
    canvas.save();
    canvas.clipPath(Path()..addOval(Rect.fromCircle(center: center, radius: r)));
    final surface = center.dy + r - 2 * r * level.clamp(0.0, 1.0);
    final speed = active ? 1.0 : 0.0;
    _wave(canvas, center, r, surface + 4, t * 0.9 * speed + 1.3, (active ? PoolGraph.give : palette.textMuted).withValues(alpha: 0.18));
    _wave(canvas, center, r, surface, t * 1.4 * speed, active ? const Color(0xFF3B5A47) : palette.border);
    canvas.restore();
    canvas.drawCircle(center, r, Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.2
      ..color = active ? PoolGraph.give.withValues(alpha: 0.55) : palette.border);
  }

  void _wave(Canvas canvas, Offset center, double r, double surface, double phase, Color color) {
    final path = Path()..moveTo(center.dx - r, center.dy + r);
    for (var x = -r; x <= r; x += 4) {
      path.lineTo(center.dx + x, surface + math.sin(x / r * math.pi * 1.6 + phase) * (layout.compact ? 3 : 4.5));
    }
    path
      ..lineTo(center.dx + r, center.dy + r)
      ..close();
    canvas.drawPath(path, Paint()..color = color);
  }

  void _dashed(Canvas canvas, Offset from, Offset to, Paint paint) {
    final total = (to - from).distance;
    final step = (to - from) / total;
    for (var at = 0.0; at < total; at += 9) {
      canvas.drawLine(from + step * at, from + step * math.min(total, at + 5), paint);
    }
  }

  @override
  bool shouldRepaint(_PoolPainter old) => old.members != members || old.level != level || old.active != active || old.layout.size != layout.size || old.palette != palette;
}

class _MemberNode extends StatelessWidget {
  const _MemberNode({
    required this.member,
    required this.size,
    required this.compact,
    required this.active,
    required this.selected,
    required this.labelAbove,
    required this.labelHeight,
    required this.onTap,
  });

  final PoolGraphMember member;
  final double size;
  final bool compact;
  final bool active;
  final bool selected;
  final bool labelAbove;
  final double labelHeight;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final lit = active && member.state == 'online';
    final words = member.name.trim().split(RegExp(r'\s+')).where((word) => word.isNotEmpty).toList();
    final initials = words.length > 1 ? '${words[0].characters.first}${words[1].characters.first}' : member.name.characters.take(2).toString();
    final dot = switch (lit ? 'online' : member.state == 'online' ? 'paused' : member.state) {
      'online' => palette.success,
      'offline' => palette.warning,
      _ => palette.textMuted,
    };
    final avatar = AnimatedContainer(
      duration: const Duration(milliseconds: 200),
      width: size,
      height: size,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        boxShadow: selected ? <BoxShadow>[BoxShadow(color: PoolGraph.give.withValues(alpha: 0.35), blurRadius: 18)] : const <BoxShadow>[],
      ),
      child: Opacity(
        opacity: lit ? 1 : 0.5,
        child: CustomPaint(
          painter: _RingPainter(share: member.share, track: palette.border, width: selected ? 3.5 : 2.5),
          child: Padding(
            padding: EdgeInsets.all(selected ? 4 : 3.5),
            child: Container(
              alignment: Alignment.center,
              decoration: BoxDecoration(shape: BoxShape.circle, color: member.isYou ? palette.accentSoft : palette.surfaceRaised),
              child: Text(initials.toUpperCase(), style: TextStyle(fontWeight: FontWeight.w700, fontSize: size * 0.28, color: member.isYou ? palette.accent : palette.text)),
            ),
          ),
        ),
      ),
    );
    final label = SizedBox(
      height: labelHeight,
      child: Column(
        mainAxisAlignment: labelAbove ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: <Widget>[
          if (!labelAbove) SizedBox(height: compact ? 4 : 6),
          Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Container(width: 7, height: 7, decoration: BoxDecoration(shape: BoxShape.circle, color: dot)),
              const SizedBox(width: 6),
              Flexible(child: Text(compact && member.isYou ? member.name : member.label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: compact ? 12 : 13.5, fontWeight: FontWeight.w600, color: palette.text))),
            ],
          ),
          if (!compact) Text(member.caption, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 11.5, color: palette.textMuted)),
          if (labelAbove) SizedBox(height: compact ? 4 : 6),
        ],
      ),
    );
    return Semantics(
      button: true,
      selected: selected,
      label: member.label,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(14),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: labelAbove ? <Widget>[label, avatar] : <Widget>[avatar, label],
        ),
      ),
    );
  }
}

/// The member's part of the shared models, as a gold arc around them.
class _RingPainter extends CustomPainter {
  const _RingPainter({required this.share, required this.track, required this.width});

  final double share;
  final Color track;
  final double width;

  @override
  void paint(Canvas canvas, Size size) {
    final rect = (Offset.zero & size).deflate(width / 2);
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = width;
    canvas.drawArc(rect, 0, 2 * math.pi, false, paint..color = track);
    if (share > 0) {
      canvas.drawArc(rect, -math.pi / 2, 2 * math.pi * share.clamp(0.0, 1.0), false, paint
        ..color = PoolGraph.give
        ..strokeCap = StrokeCap.round);
    }
  }

  @override
  bool shouldRepaint(_RingPainter old) => old.share != share || old.track != track || old.width != width;
}
