import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import { ConfirmModal } from '../components/ui/ConfirmModal';

interface ModalState {
  isOpen: boolean;
  title: string;
  message: string;
  type?: 'info' | 'warning' | 'error' | 'success';
  confirmText?: string;
  cancelText?: string;
  confirmButtonClass?: string;
  onConfirm?: () => void;
  onCancel?: () => void;
}

interface ModalContextValue {
  confirm: (options: {
    title: string;
    message: string;
    type?: 'info' | 'warning' | 'error' | 'success';
    confirmText?: string;
    cancelText?: string;
    confirmButtonClass?: string;
  }) => Promise<boolean>;
}

const ModalContext = createContext<ModalContextValue | undefined>(undefined);

interface ModalProviderProps {
  children: ReactNode;
}

export function ModalProvider({ children }: ModalProviderProps) {
  const [modalState, setModalState] = useState<ModalState>({
    isOpen: false,
    title: '',
    message: '',
    type: 'warning',
  });

  const confirm = useCallback(
    (options: {
      title: string;
      message: string;
      type?: 'info' | 'warning' | 'error' | 'success';
      confirmText?: string;
      cancelText?: string;
      confirmButtonClass?: string;
    }): Promise<boolean> => {
      return new Promise((resolve) => {
        setModalState({
          isOpen: true,
          title: options.title,
          message: options.message,
          type: options.type || 'warning',
          confirmText: options.confirmText,
          cancelText: options.cancelText,
          confirmButtonClass: options.confirmButtonClass,
          onConfirm: () => {
            setModalState(prev => ({ ...prev, isOpen: false }));
            resolve(true);
          },
          onCancel: () => {
            setModalState(prev => ({ ...prev, isOpen: false }));
            resolve(false);
          },
        });
      });
    },
    []
  );

  const handleClose = useCallback(() => {
    if (modalState.onCancel) {
      modalState.onCancel();
    } else {
      setModalState(prev => ({ ...prev, isOpen: false }));
    }
  }, [modalState.onCancel]);

  const value: ModalContextValue = {
    confirm,
  };

  return (
    <ModalContext.Provider value={value}>
      {children}
      <ConfirmModal
        isOpen={modalState.isOpen}
        title={modalState.title}
        message={modalState.message}
        type={modalState.type}
        confirmText={modalState.confirmText}
        cancelText={modalState.cancelText}
        confirmButtonClass={modalState.confirmButtonClass}
        onConfirm={modalState.onConfirm || (() => {})}
        onCancel={modalState.onCancel || handleClose}
      />
    </ModalContext.Provider>
  );
}

export function useModal() {
  const context = useContext(ModalContext);
  if (context === undefined) {
    throw new Error('useModal must be used within a ModalProvider');
  }
  return context;
}

// Convenience hook for common confirmation patterns
export function useConfirm() {
  const { confirm } = useModal();
  
  const confirmDelete = useCallback(
    (itemName?: string) => confirm({
      title: 'Confirm Delete',
      message: itemName 
        ? `Are you sure you want to delete "${itemName}"? This action cannot be undone.`
        : 'Are you sure you want to delete this item? This action cannot be undone.',
      type: 'error',
      confirmText: 'Delete',
      confirmButtonClass: 'btn-error',
    }),
    [confirm]
  );

  const confirmAction = useCallback(
    (title: string, message: string, options?: {
      type?: 'info' | 'warning' | 'error' | 'success';
      confirmText?: string;
      confirmButtonClass?: string;
    }) => confirm({
      title,
      message,
      type: options?.type || 'warning',
      confirmText: options?.confirmText || 'Confirm',
      confirmButtonClass: options?.confirmButtonClass || 'btn-primary',
    }),
    [confirm]
  );

  return {
    confirm,
    confirmDelete,
    confirmAction,
  };
}