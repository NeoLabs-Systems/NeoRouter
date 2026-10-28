import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

class NeoRouterAuthScreen extends StatefulWidget {
  const NeoRouterAuthScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<NeoRouterAuthScreen> createState() => _NeoRouterAuthScreenState();
}

class _NeoRouterAuthScreenState extends State<NeoRouterAuthScreen> {
  final TextEditingController _usernameController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _confirmPasswordController =
      TextEditingController();
  final TextEditingController _codeController = TextEditingController();
  late bool _registerMode = widget.controller.bootstrapRegistration;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _emailController.dispose();
    _confirmPasswordController.dispose();
    _codeController.dispose();
    super.dispose();
  }

  Future<void> _showForgotPasswordDialog() async {
    final t = AppL10n.of(context);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.forgotPasswordTitle),
        content: SizedBox(
          width: 420,
          child: SelectableText(
            t.forgotPasswordBody,
            style: TextStyle(
              color: neoRouterPaletteOf(context).textSoft,
              height: 1.45,
            ),
          ),
        ),
        actions: <Widget>[
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: Text(t.close),
          ),
        ],
      ),
    );
  }

  Future<void> _submit() async {
    final t = AppL10n.of(context);
    if (_registerMode) {
      if (_passwordController.text != _confirmPasswordController.text) {
        widget.controller.showAuthError(t.passwordsDontMatch);
        return;
      }
      if (_passwordController.text.length < 12) {
        widget.controller.showAuthError(t.passwordTooShort);
        return;
      }
      await widget.controller.register(
        _usernameController.text.trim(),
        _emailController.text.trim(),
        _passwordController.text,
      );
      return;
    }
    await widget.controller.login(
      _usernameController.text.trim(),
      _passwordController.text,
    );
  }

  Future<void> _submitCode() async {
    final code = _codeController.text.trim();
    if (code.isEmpty) return;
    final ok = await widget.controller.completeTwoFactor(code);
    if (!ok) _codeController.clear();
  }

  List<Widget> _twoFactorFields(NeoRouterController controller, AppL10n t) {
    return <Widget>[
      TextField(
        controller: _codeController,
        autofocus: true,
        keyboardType: TextInputType.visiblePassword,
        autofillHints: const <String>[AutofillHints.oneTimeCode],
        onSubmitted: (_) => _submitCode(),
        decoration: InputDecoration(
          labelText: t.twoFactorCode,
          helperText: t.twoFactorCodeHint,
        ),
      ),
      if (controller.authError != null) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        InlineError(message: controller.authError!),
      ],
      const SizedBox(height: AppSpacing.lg),
      FilledButton(
        onPressed: controller.loading ? null : _submitCode,
        child: controller.loading
            ? const SizedBox.square(
                dimension: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Text(t.twoFactorVerify),
      ),
      const SizedBox(height: AppSpacing.sm),
      TextButton(
        onPressed: controller.loading
            ? null
            : () {
                _codeController.clear();
                controller.cancelTwoFactor();
              },
        child: Text(t.twoFactorBack),
      ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final controller = widget.controller;
    final t = AppL10n.of(context);
    final bootstrap = controller.bootstrapRegistration;
    final twoFactor = controller.awaitingTwoFactor;
    final title = twoFactor
        ? t.twoFactorLoginTitle
        : bootstrap
        ? t.authBootstrapTitle
        : (_registerMode ? t.authCreate : t.authWelcome);
    final subtitle = twoFactor
        ? t.twoFactorLoginSubtitle
        : bootstrap
        ? t.authBootstrapSubtitle
        : (_registerMode ? t.authRegisterSubtitle : t.authLoginSubtitle);

    return AuthBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: LayoutBuilder(
            builder: (context, viewportConstraints) {
              return SingleChildScrollView(
                child: ConstrainedBox(
                  constraints: BoxConstraints(
                    minHeight: viewportConstraints.maxHeight,
                  ),
                  child: Padding(
                    padding: EdgeInsets.all(
                      viewportConstraints.maxWidth < AppBreakpoints.mobile
                          ? 14
                          : 24,
                    ),
                    child: Center(
                      child: EntranceMotion(
                        child: GlassSurface(
                          width: 420,
                          padding: const EdgeInsets.all(AppSpacing.xl),
                          child: AutofillGroup(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: <Widget>[
                                const Center(
                                  child: BrandLockup(
                                    logoSize: 44,
                                    showName: false,
                                  ),
                                ),
                                const SizedBox(height: AppSpacing.md),
                                Text(
                                  title,
                                  style: displayTitleStyle(palette, size: 24),
                                ),
                                const SizedBox(height: AppSpacing.sm),
                                Text(
                                  subtitle,
                                  style: Theme.of(context).textTheme.bodyMedium
                                      ?.copyWith(color: palette.textSoft),
                                ),
                                const SizedBox(height: AppSpacing.lg),
                                if (controller.awaitingTwoFactor)
                                  ..._twoFactorFields(controller, t)
                                else ...<Widget>[
                                TextField(
                                  controller: _usernameController,
                                  autofillHints: const <String>[
                                    AutofillHints.username,
                                  ],
                                  decoration: InputDecoration(
                                    labelText: _registerMode
                                        ? t.username
                                        : t.usernameOrEmail,
                                  ),
                                ),
                                if (_registerMode) ...<Widget>[
                                  const SizedBox(height: AppSpacing.sm),
                                  TextField(
                                    controller: _emailController,
                                    keyboardType: TextInputType.emailAddress,
                                    autofillHints: const <String>[
                                      AutofillHints.email,
                                    ],
                                    decoration: InputDecoration(
                                      labelText: t.email,
                                    ),
                                  ),
                                ],
                                const SizedBox(height: AppSpacing.sm),
                                TextField(
                                  controller: _passwordController,
                                  obscureText: true,
                                  autofillHints: <String>[
                                    _registerMode
                                        ? AutofillHints.newPassword
                                        : AutofillHints.password,
                                  ],
                                  onSubmitted: (_) =>
                                      _registerMode ? null : _submit(),
                                  decoration: InputDecoration(
                                    labelText: t.password,
                                  ),
                                ),
                                if (_registerMode) ...<Widget>[
                                  const SizedBox(height: AppSpacing.sm),
                                  TextField(
                                    controller: _confirmPasswordController,
                                    obscureText: true,
                                    onSubmitted: (_) => _submit(),
                                    decoration: InputDecoration(
                                      labelText: t.confirmPassword,
                                    ),
                                  ),
                                ],
                                if (controller.authError != null) ...<Widget>[
                                  const SizedBox(height: AppSpacing.sm),
                                  InlineError(message: controller.authError!),
                                ],
                                const SizedBox(height: AppSpacing.lg),
                                FilledButton(
                                  onPressed: controller.loading
                                      ? null
                                      : _submit,
                                  child: controller.loading
                                      ? const SizedBox.square(
                                          dimension: 16,
                                          child: CircularProgressIndicator(
                                            strokeWidth: 2,
                                          ),
                                        )
                                      : Text(
                                          _registerMode
                                              ? t.createAccount
                                              : t.signIn,
                                        ),
                                ),
                                if (!bootstrap) ...<Widget>[
                                  const SizedBox(height: AppSpacing.sm),
                                  Wrap(
                                    alignment: WrapAlignment.spaceBetween,
                                    crossAxisAlignment:
                                        WrapCrossAlignment.center,
                                    children: <Widget>[
                                      TextButton(
                                        onPressed: controller.loading
                                            ? null
                                            : () {
                                                if (!_registerMode &&
                                                    !controller
                                                        .registrationOpen) {
                                                  controller.showAuthError(
                                                    t.registrationClosed,
                                                  );
                                                  return;
                                                }
                                                setState(
                                                  () => _registerMode =
                                                      !_registerMode,
                                                );
                                              },
                                        child: Text(
                                          _registerMode
                                              ? t.toLogin
                                              : t.toRegister,
                                        ),
                                      ),
                                      if (!_registerMode)
                                        TextButton(
                                          onPressed: controller.loading
                                              ? null
                                              : _showForgotPasswordDialog,
                                          child: Text(t.forgotPassword),
                                        ),
                                    ],
                                  ),
                                ],
                                ],
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }
}
