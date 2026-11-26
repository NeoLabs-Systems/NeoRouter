import 'package:flutter/material.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoCredentialField, NeoCredentialFields, NeoForgotPasswordDialog;

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_qr_signin.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';

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
  final FocusNode _usernameFocus = FocusNode();
  final FocusNode _emailFocus = FocusNode();
  final FocusNode _passwordFocus = FocusNode();
  final FocusNode _confirmFocus = FocusNode();
  late bool _registerMode = widget.controller.bootstrapRegistration;
  bool _obscure = true;
  bool _showQr = false;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _emailController.dispose();
    _confirmPasswordController.dispose();
    _codeController.dispose();
    _usernameFocus.dispose();
    _emailFocus.dispose();
    _passwordFocus.dispose();
    _confirmFocus.dispose();
    super.dispose();
  }

  Future<void> _showForgotPasswordDialog() async {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final palette = neoRouterPaletteOf(context);
    final messenger = ScaffoldMessenger.of(context);
    await showDialog<void>(
      context: context,
      builder: (_) => NeoForgotPasswordDialog(
        initialAccount: _usernameController.text.trim(),
        title: t.forgotPasswordTitle,
        description: t.forgotPasswordDescription,
        accountLabel: t.usernameOrEmail,
        emptyError: t.forgotPasswordMissing,
        cancelLabel: t.cancel,
        submitLabel: t.forgotPasswordSubmit,
        onSubmit: (account) async {
          String? failure;
          try {
            await controller.auth.forgotPassword(controller.client.baseUrl, account);
          } on ApiException catch (error) {
            failure = error.message;
          } catch (_) {
            failure = t.serverUnreachable;
          }
          messenger.showSnackBar(SnackBar(content: Text(failure ?? t.forgotPasswordSent)));
          return failure == null;
        },
        errorBuilder: (message) => InlineError(message: message),
        backgroundColor: palette.surface,
        textColor: palette.textSoft,
      ),
    );
  }

  Widget _credentials(NeoRouterController controller, AppL10n t) {
    final palette = neoRouterPaletteOf(context);
    String label(NeoCredentialField field) => switch (field) {
      NeoCredentialField.username => _registerMode ? t.username : t.usernameOrEmail,
      NeoCredentialField.email => t.email,
      NeoCredentialField.password => t.password,
      NeoCredentialField.confirmPassword => t.confirmPassword,
    };
    return NeoCredentialFields(
      register: _registerMode,
      username: _usernameController,
      email: _emailController,
      password: _passwordController,
      confirmPassword: _confirmPasswordController,
      usernameFocus: _usernameFocus,
      emailFocus: _emailFocus,
      passwordFocus: _passwordFocus,
      confirmPasswordFocus: _confirmFocus,
      obscurePassword: _obscure,
      onChanged: (_) {},
      onSubmit: _submit,
      decoration: (field, suffix) => InputDecoration(labelText: label(field), suffixIcon: suffix),
      // The labels float inside the fields, so a field is its own labelled row.
      labeledField: (_, field, trailing) =>
          trailing == null ? field : Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: <Widget>[field, trailing]),
      usernameLabel: t.username,
      usernameOrEmailLabel: t.usernameOrEmail,
      emailLabel: t.email,
      passwordLabel: t.password,
      confirmPasswordLabel: t.confirmPassword,
      passwordToggle: IconButton(
        tooltip: _obscure ? t.showPassword : t.hidePassword,
        onPressed: () => setState(() => _obscure = !_obscure),
        icon: Icon(_obscure ? Icons.visibility_outlined : Icons.visibility_off_outlined, size: 18),
      ),
      passwordStrength: Text(t.passwordRules, style: TextStyle(color: palette.textMuted, fontSize: 12, height: 1.4)),
      forgotPassword: controller.passwordResetAvailable
          ? Align(
              alignment: Alignment.centerRight,
              child: TextButton(onPressed: controller.loading ? null : _showForgotPasswordDialog, child: Text(t.forgotPassword)),
            )
          : null,
    );
  }

  /// Passkey and phone sign-in, once the server said what it offers.
  List<Widget> _otherWays(NeoRouterController controller, AppL10n t) {
    if (!controller.authOptionsLoaded || _registerMode) return const <Widget>[];
    return <Widget>[
      if (controller.webAuthn.isSupported) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        OutlinedButton.icon(
          onPressed: controller.loading ? null : () => controller.signInWithPasskey(_usernameController.text.trim()),
          icon: const Icon(Icons.key_rounded, size: 16),
          label: Text(t.passkeySignIn),
        ),
      ],
      const SizedBox(height: AppSpacing.sm),
      OutlinedButton.icon(
        onPressed: () => setState(() => _showQr = !_showQr),
        icon: const Icon(Icons.qr_code_rounded, size: 16),
        label: Text(t.qrSignIn),
      ),
      if (_showQr) ...<Widget>[const SizedBox(height: AppSpacing.md), QrSignInPanel(controller: controller)],
    ];
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
        InlineError(message: controller.authErrorCode == 'INVALID_TWO_FACTOR' ? t.accountWrongCode : controller.authError!),
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
                                _credentials(controller, t),
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
                                ..._otherWays(controller, t),
                                if (!bootstrap) ...<Widget>[
                                  const SizedBox(height: AppSpacing.sm),
                                  TextButton(
                                    onPressed: controller.loading
                                        ? null
                                        : () {
                                            if (!_registerMode && !controller.registrationOpen) {
                                              controller.showAuthError(t.registrationClosed);
                                              return;
                                            }
                                            setState(() => _registerMode = !_registerMode);
                                          },
                                    child: Text(_registerMode ? t.toLogin : t.toRegister),
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
