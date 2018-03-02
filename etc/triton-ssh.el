;;; triton-ssh.el ---                            -*- lexical-binding: t; -*-

;; Copyright (C) 2017  Seong-Kook Shin

;; Author: Seong-Kook Shin <cinsky@gmail.com>
;; Keywords:

;; This program is free software; you can redistribute it and/or modify
;; it under the terms of the GNU General Public License as published by
;; the Free Software Foundation, either version 3 of the License, or
;; (at your option) any later version.

;; This program is distributed in the hope that it will be useful,
;; but WITHOUT ANY WARRANTY; without even the implied warranty of
;; MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
;; GNU General Public License for more details.

;; You should have received a copy of the GNU General Public License
;; along with this program.  If not, see <http://www.gnu.org/licenses/>.

;;; Commentary:

;;
;; You need to build the binary of triton-pssh to use this script.
;; Set up Go build environment; see https://golang.org/doc/install
;;
;; Set up GOPATH (if you haven't) and build triton-pssh:
;;
;;   $ export GOPATH=$HOME/go
;;   $ go get github.com/cinsk/triton-pssh
;;
;; To install this packge, add following in your $HOME/.emacs,
;; or $HOME/.emacs.d/init.el:
;;
;;   (add-to-list 'load-path (expand-file-name
;;                             (concat (getenv "GOPATH")
;;                               "/src/github.com/cinsk/triton-pssh/emacs/triton-ssh.el"))
;;   (require 'triton-ssh)
;;
;;
;; M-x triton-ssh <RET> will ask you the filter-expression to select the Triton machine, and
;; C-u M-x triton-ssh <RET>  will ask you to select the Triton profile first.
;; C-u C-u M-x triton-ssh <RET> is the same as C-u M-x triton-ssh but it updates the cache first.
;;
;; `triton-ssh' uses `term-mode' for the buffer. Since it uses character mode, most
;; Emacs keybinding may not work. (the keystroke will go to the SSH session)
;; Here's some useful shortcuts of `term-mode':
;;
;;    C-c       escape sequence.  Behaves like `C-x',
;;    C-c M-x   Behaves like `M-x'
;;    C-c C-j   switch to line mode.  Once switched, the buffer behaves like `shell-mode'.
;;    C-c C-k   switch to char mode.  (default)
;;    C-c C-c   send ^C to the process.
;;

;;; Code:


(require 'term)
(require 'json)

(defvar triton-profile (getenv "TRITON_PROFILE")
  "The name of the current profile in Triton CLI")

(defvar triton-ssh-program (concat (file-name-as-directory (getenv "GOPATH"))
                                   "src/github.com/cinsk/triton-pssh/etc/triton-ssh.sh")
  "Pathname of executable of triton-ssh.sh")

(defvar triton-pssh-program (concat (file-name-as-directory (getenv "GOPATH"))
                                   "bin/triton-pssh")
  "Pathname of executable of triton-ssh.sh")

(defvar triton-ssh-use-completion t
  "Use experimental completion feature")

(defun triton-ssh--current-profile (&optional ask)
  (when ask
    (setq triton-profile
          (completing-read "profile: "
                           (split-string
                            (shell-command-to-string
                             "triton profile ls -H -o name")))))
  triton-profile)

(defun triton-ssh--buffer-name (triton-ssh-arguments)
  (let ((tokens (triton-ssh--parse-words triton-ssh-arguments)))
    (if (eq (length tokens) 0)
        "NONAME"
      (nth (1- (length tokens)) tokens))))


(defun triton-ssh--parse-words (line)
  "Steaded from `ssh-parse-word` from ssh package"
  (let ((list nil)
        (text nil)
        buf)
    (unwind-protect
        (save-match-data
          (save-excursion
            (setq buf (generate-new-buffer " *ssh-parse-words*"))
            (set-buffer buf)
            (insert line)
            (goto-char (point-min))
            (while (not (eobp))
              (setq text nil)
              (and (looking-at "\\`[ \t]+")
                   (narrow-to-region (match-end 0) (point-max)))
              (cond ((looking-at "\\`\\(['\"]\\)\\([^\\1]+\\)\\1")
                     (setq text (buffer-substring (match-beginning 2)
                                                  (match-end 2))))
                    ((looking-at "\\`[^ \t]+")
                     (setq text (buffer-substring (point-min) (match-end 0)))))
              (narrow-to-region (match-end 0) (point-max))
              (and text (setq list (cons text list))))))
      (kill-buffer buf))
    (nreverse list)))


(defun triton-ssh (profile triton-ssh-arguments)
  "Open a SSH session to a Triton machine.

If a prefix argument is given, `triton-ssh' will ask the current
Triton profile to use.

Internally, this command uses \"triton-ssh.sh\" that is shipped
in triton-pssh Go package.  https://github.com/cinsk/triton-pssh

For example, to connect a machine named foo, you provide \"foo\"
for TRITON-SSH-ARGUMENT.  If that machine requires a Bastion
server bar, you provide \"-b bar foo\".
"
  (interactive (list
                (triton-ssh--current-profile current-prefix-arg)
                (progn
                  (triton-ssh--update-cache current-prefix-arg)
                  (triton-ssh-read-from-minibuffer "triton-ssh command line (e.g. [-b bastion] -h hostname): "
                                                   nil nil nil 'triton-ssh-history))))
  (let ((cmdlines (format "eval \"$(triton env %s)\"; %s %s"
                          profile triton-ssh-program triton-ssh-arguments))
        (bufname (format "ssh:%s"
                         (triton-ssh--buffer-name triton-ssh-arguments))))
    (let ((buf (make-term bufname "/bin/bash" nil "-c" cmdlines)))
      (with-current-buffer buf
        (term-mode)
        (term-char-mode)
        (goto-char (point-max)))
      (switch-to-buffer buf)))
  (message "Prefix command is 'C-c'.  C-c C-j for line mode, C-c C-k for char mode."))


(defun triton-ssh--update-cache (prefix)
  (let ((arg (if (listp prefix)
                 (car prefix)
               prefix)))
    ;; (message "prefix: %S" arg)
    (if (and (integerp arg) (eq arg 16))
        (condition-case nil
            (progn (message "Updating cache...")
                   (call-process triton-pssh-program nil nil nil "--no-cache" "-1" "true"))
          (error nil)))))

(defun triton-cached-instances ()
  (let ((sources (directory-files (concat (file-name-as-directory (expand-file-name "~/.triton-pssh/cache"))
                                          (file-name-as-directory triton-profile)
                                          "instances")
                                  'full-name "[0-9]*-[0-9]*"))
        instances)
    (dolist (f sources instances)
      (setq instances (append instances (mapcar (lambda (x) (cdr (assoc 'name x))) (json-read-file f)))))))

;;
;; TODO:
;;
;; write a completion function so that it can accept generalized data structure like this:
;;
;; (("-h" . file)
;;  ("-b" . file)
;;  ("--bastion=" . file))
;;
(defun triton-ssh--completing-command-line (s)
  (let* ((tokens (split-string s " +"))
         (len (length tokens))
         (last (nth (1- len) tokens))
         (prev (if (> len 1) (nth (- len 2) tokens) "")))
    (cond ((string-match "-[a-zA-Z0-9]" last)
           '(" "))
          ((or (string-equal prev "-h") (string-equal prev "-b"))
           (cl-remove-if 'null
                         (mapcar (lambda (ent)
                                   (if (and (string-prefix-p last ent)) ;; (< (length last) (length ent)))
                                       (substring ent (length last))))
                                 (triton-cached-instances)))))))

(defun triton-ssh-read-from-minibuffer (prompt &optional initial keymap read-hist default-value inherit input-method)
  (if (not triton-ssh-use-completion)
      (read-from-minibuffer prompt initial keymap read-hist default-value inherit input-method)
    (minibuffer-with-setup-hook (lambda ()
                                  (local-set-key (kbd "SPC") 'self-insert-command))
      (completing-read prompt (lambda (s pred flag)
                                 (let ((cands (triton-ssh--completing-command-line s)))
                                   (setq cands (mapcar (lambda (x) (concat s x)) cands))
                                   ;; (message-log "cands: %S" cands)
                                   (complete-with-action flag cands s pred)))
                       nil nil))))


(defun message-log (&rest args)
  (with-current-buffer (get-buffer "*Messages*")
    (let ((inhibit-read-only t)
          (msg (apply 'format args)))
      (goto-char (point-max))
      (insert (concat msg "\n")))))

(when nil
  (minibuffer-with-setup-hook (lambda ()
                                (local-set-key (kbd "SPC") 'self-insert-command))
    (completing-read "arg: " (lambda (s pred flag)
                               (let ((cands (triton-ssh--completing-command-line s)))
                                 (setq cands (mapcar (lambda (x) (concat s x)) cands))
                                 (message-log "cands: %S" cands)
                                 (complete-with-action flag cands s pred)))
                     nil nil)))


(provide 'triton-ssh)
;;; triton.el ends here
